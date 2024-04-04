package server

import (
	"errors"
	"fmt"
	"sort"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gliderlabs/ssh"
	log "github.com/sirupsen/logrus"
	"kuberstein.io/ingressh/internal/types"
)

var docStyle = lipgloss.NewStyle().Margin(1, 2)

type item string

func (i item) FilterValue() string { return "" }

type selectionState int

const (
	selectNamespace selectionState = iota
	selectPod
	selectContainer
)

type model struct {
	state      selectionState
	stateNoWay error

	listNamespaces  list.Model
	listPods        list.Model
	listPodsConfigs []podSshConfig
	listContainers  list.Model

	choiceNamespace string
	choicePod       string
	choicePodConfig podSshConfig
	choiceContainer string

	quittingWithError error

	targetAuth authz
	hint       types.SshTarget
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) activeList() *list.Model {
	var a *list.Model
	switch m.state {
	case selectNamespace:
		a = &m.listNamespaces
	case selectPod:
		a = &m.listPods
	case selectContainer:
		a = &m.listContainers
	}
	return a
}

func setupList(items []list.Item, title string) list.Model {
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = title
	return l
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	activeList := m.activeList()

	// "Nothing here" screen
	if m.stateNoWay != nil {
		switch msg.(type) {
		case tea.KeyMsg:
			// "Nothing here" screen is shown. Stays on the current list as
			// stateNoWay is raised when the next list for the selected object
			// can't be created. Drop the flag for the "nothing here" screen.
			m.stateNoWay = nil
		}
		return m, nil
	}

	// Key handlers
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		activeList.SetSize(msg.Width-h, msg.Height-v)

	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "enter":
			i, ok := activeList.SelectedItem().(item)
			if !ok {
				return m, tea.Quit
			}
			switch m.state {
			case selectNamespace:
				m.stateNoWay = m.startSelectPodScreen()
			case selectPod:
				m.stateNoWay = m.startSelectContainerScreen()
			case selectContainer:
				m.choiceContainer = string(i)
				return m, tea.Quit
			}
			return m, nil
		case "esc":
			// Escape brings us to the previous state of the selection wizard
			switch m.state {
			case selectPod:
				m.state = selectNamespace
				m.choiceNamespace = ""
			case selectContainer:
				m.state = selectPod
				m.choicePod = ""
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	*activeList, cmd = activeList.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.stateNoWay != nil {
		return docStyle.Render(fmt.Sprintf(
			"No authorized objects: %s\n\nPress any key to select a different option\n", m.stateNoWay))
	}

	if m.choiceContainer != "" {
		return docStyle.Render(fmt.Sprintf(
			"Proceed with %s/%s/%s...\n", m.choiceNamespace, m.choicePod, m.choiceContainer))
	}
	if m.quittingWithError != nil {
		return docStyle.Render(fmt.Sprintf("Error setting up SSH session: %v\n", m.quittingWithError))
	}

	activeList := m.activeList()
	return docStyle.Render(activeList.View())
}

func (m *model) startSelectPodScreen() error {
	targetNamespace := string(m.listNamespaces.SelectedItem().(item))
	podConfigs, err := m.targetAuth.GetPods(targetNamespace, m.hint.Pod)
	if err != nil {
		return err
	}
	if len(podConfigs) == 0 {
		return fmt.Errorf("no authorized pods in namespace %s", targetNamespace)
	}

	m.choiceNamespace = targetNamespace
	m.listPodsConfigs = podConfigs

	sort.Slice(podConfigs, func(i, j int) bool {
		return podConfigs[i].pod.Name < podConfigs[j].pod.Name
	})

	items := []list.Item{}
	for _, p := range podConfigs {
		items = append(items, item(p.pod.Name))
	}

	m.listPods = setupList(items, fmt.Sprintf("Pods in the namespace `%s`", targetNamespace))
	m.state = selectPod

	// When there is actually no choice - select the only element automatically
	// and advance to the next selection
	if len(podConfigs) == 1 {
		m.listPods.Select(0)
		m.choicePod = podConfigs[0].pod.Name
		m.choicePodConfig = podConfigs[0]
		return m.startSelectContainerScreen()
	}

	return nil
}

func (m *model) startSelectContainerScreen() error {
	podConfigIdx := m.listPods.Index()

	selectedPodConfig := m.listPodsConfigs[podConfigIdx]
	pod := selectedPodConfig.pod
	config := selectedPodConfig.config
	containers, err := m.targetAuth.GetContainers(pod, config.Containers, m.hint.Container)
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		return fmt.Errorf("no authorized containers in pod %s", pod.Name)
	}

	m.choicePod = string(m.listPods.SelectedItem().(item))
	m.choicePodConfig = m.listPodsConfigs[podConfigIdx]

	sort.Strings(containers)
	items := []list.Item{}
	for _, c := range containers {
		items = append(items, item(c))
	}

	m.listContainers = setupList(items, fmt.Sprintf("Containers in `%s`/`%s`", m.choiceNamespace, m.choicePod))
	m.state = selectContainer

	// When there is actually no choice - select the only element automatically
	// and proceed to exit
	if len(containers) == 1 {
		m.listContainers.Select(0)
		m.choiceContainer = containers[0]
		return nil
	}

	return nil
}

func (m *model) startSelectNamespaceScreen() error {
	namespaces, err := m.targetAuth.GetNamespaces(m.hint.Namespace)
	if err != nil {
		return err
	}
	if len(namespaces) == 0 {
		return errors.New("no authorized namespaces")
	}

	sort.Strings(namespaces)
	items := []list.Item{}
	for _, ns := range namespaces {
		items = append(items, item(ns))
	}

	m.listNamespaces = setupList(items, "Namespaces")
	m.state = selectNamespace

	// When there is actually no choice - select the only element automatically
	// and advance to the next selection screen
	if len(namespaces) == 1 {
		m.listNamespaces.Select(0)
		m.choiceNamespace = namespaces[0]
		return m.startSelectPodScreen()
	}

	return nil
}

func (m model) result() (types.SshTarget, podSshConfig) {
	r := types.SshTarget{
		Namespace: m.choiceNamespace,
		Pod:       m.choicePod,
		Container: m.choiceContainer,
	}
	return r, m.choicePodConfig
}

// Returns attach target and pod+configuration as a result of the
// user's interactive selection.
// If the user have specified hint information, the appropriate filtering
// is applied to the selection lists.
func interactive(sess ssh.Session, targetAuth authz, hint types.SshTarget) (
	types.SshTarget, podSshConfig, error,
) {
	m := model{
		targetAuth: targetAuth,
		hint:       hint,
	}

	err := m.startSelectNamespaceScreen()
	if err != nil {
		return types.SshTarget{}, podSshConfig{}, err
	}

	// Shortcut if the target selection is unambiguous and was computed
	// at the first screen already
	r, c := m.result()
	if r.IsComplete() {
		return r, c, nil
	}

	p := tea.NewProgram(m, tea.WithOutput(sess), tea.WithInput(sess))
	result, err := p.Run()
	if err != nil {
		log.Errorf("Error on interactive access target select: %v", err)
		return types.SshTarget{}, podSshConfig{}, err
	}

	fmt.Fprint(sess, "\n")
	sshTarget, podConfig := result.(model).result()
	return sshTarget, podConfig, nil
}
