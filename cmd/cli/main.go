package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/gorilla/websocket"
)

// --- 🎨 样式定义 ---
var (
	colorUser   = lipgloss.Color("#00FFFF") // 青色
	colorBot    = lipgloss.Color("#00FF00") // 矩阵绿
	colorSystem = lipgloss.Color("#555555") // 灰色
	colorStep   = lipgloss.Color("#AAAAAA") // 浅灰

	senderStyle = lipgloss.NewStyle().Foreground(colorUser).Bold(true).MarginTop(1)
	botStyle    = lipgloss.NewStyle().Foreground(colorBot).Bold(true).MarginTop(1)
	stepStyle   = lipgloss.NewStyle().Foreground(colorStep).Italic(true).MarginLeft(2)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)

	sessionID = fmt.Sprintf("cli-%d", time.Now().Unix())
)

// --- 消息协议 ---
type StreamMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// --- Model ---
type model struct {
	viewport  viewport.Model
	textInput textinput.Model
	spinner   spinner.Model
	conn      *websocket.Conn

	connected bool

	// 🔥 核心修复：使用 string 而非 strings.Builder
	// 这是一个普通的字符串，拷贝它是安全的
	currentResponse string

	// 历史记录列表
	chatHistory []string

	// Markdown 渲染器 (指针类型，拷贝安全)
	mdRenderer *glamour.TermRenderer
	err        error
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Ask Fin-Nexus (e.g. 'Analyze NVDA')..."
	ti.Focus()
	ti.CharLimit = 200
	ti.Width = 30
	ti.PromptStyle = lipgloss.NewStyle().Foreground(colorUser)

	vp := viewport.New(100, 20)
	vp.SetContent("🔌 Initializing Uplink to Fin-Nexus Core...")

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(colorBot)

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(80),
	)

	return model{
		textInput:       ti,
		viewport:        vp,
		spinner:         s,
		mdRenderer:      renderer,
		chatHistory:     []string{},
		currentResponse: "", // 初始化为空字符串
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick, connectWS)
}

// --- WebSocket ---
func connectWS() tea.Msg {
	u := url.URL{Scheme: "ws", Host: "localhost:8080", Path: "/api/v1/ws/chat", RawQuery: "session_id=" + sessionID}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return errMsg(err)
	}
	return connMsg{c}
}

type connMsg struct{ conn *websocket.Conn }
type errMsg error
type wsMsg StreamMessage

func waitForMessage(conn *websocket.Conn) tea.Cmd {
	return func() tea.Msg {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return errMsg(err)
		}
		var msg StreamMessage
		json.Unmarshal(message, &msg)
		return wsMsg(msg)
	}
}

// --- Update ---
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
		spCmd tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 5
		m.textInput.Width = msg.Width
		m.mdRenderer, _ = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(msg.Width-10),
		)

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			if m.connected && m.textInput.Value() != "" {
				input := m.textInput.Value()

				// 1. 如果上一轮机器人的回复还在缓冲区，先固化到历史记录
				if m.currentResponse != "" {
					rendered, _ := m.mdRenderer.Render(m.currentResponse)
					m.chatHistory = append(m.chatHistory, botStyle.Render("NEXUS:")+"\n"+rendered)
					m.currentResponse = ""
				}

				// 2. 发送新消息
				m.conn.WriteMessage(websocket.TextMessage, []byte(input))

				// 3. 渲染用户消息
				m.chatHistory = append(m.chatHistory, senderStyle.Render("YOU: ")+input)

				// 4. 更新视图并清空输入
				m.viewport.SetContent(strings.Join(m.chatHistory, "\n"))
				m.viewport.GotoBottom()
				m.textInput.SetValue("")
			}
		}

	case connMsg:
		m.conn = msg.conn
		m.connected = true
		sysMsg := lipgloss.NewStyle().Foreground(colorSystem).Render("✅ Uplink Established. Ready.")
		m.chatHistory = append(m.chatHistory, sysMsg)
		m.viewport.SetContent(strings.Join(m.chatHistory, "\n"))
		// 连接成功后，立即清空输入框，防止缓冲区有残留字符
		m.textInput.SetValue("")
		return m, waitForMessage(m.conn)

	case wsMsg:
		switch msg.Type {
		case "step":
			// 步骤消息
			stepLine := stepStyle.Render(">> " + msg.Content)
			m.chatHistory = append(m.chatHistory, stepLine)
			// 强制刷新视图，确保用户看到步骤
			m.viewport.SetContent(strings.Join(m.chatHistory, "\n"))
			m.viewport.GotoBottom()

		case "token":
			// 🔥 修复点：直接用字符串拼接，安全！
			m.currentResponse += msg.Content

			// 实时渲染 Markdown
			rendered, err := m.mdRenderer.Render(m.currentResponse)
			if err != nil {
				rendered = m.currentResponse
			}

			// 视图 = 历史记录 + NEXUS头 + 当前正在生成的渲染结果
			fullView := strings.Join(m.chatHistory, "\n") + "\n" + botStyle.Render("NEXUS:") + "\n" + rendered

			m.viewport.SetContent(fullView)
			m.viewport.GotoBottom()

		case "error":
			m.chatHistory = append(m.chatHistory, errStyle.Render("ERROR: "+msg.Content))
			m.viewport.SetContent(strings.Join(m.chatHistory, "\n"))
		}
		return m, waitForMessage(m.conn)

	case errMsg:
		m.err = msg
		return m, tea.Quit
	}

	m.textInput, tiCmd = m.textInput.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)
	m.spinner, spCmd = m.spinner.Update(msg)

	return m, tea.Batch(tiCmd, vpCmd, spCmd)
}

// --- View ---
func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	status := "🔴 Disconnected"
	if m.connected {
		status = "🟢 Online | Fin-Nexus System"
	}
	if m.currentResponse != "" {
		status += fmt.Sprintf(" | %s Processing Data Stream...", m.spinner.View())
	}

	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginTop(1)

	return fmt.Sprintf(
		"%s\n%s\n%s",
		m.viewport.View(),
		statusStyle.Render(status),
		m.textInput.View(),
	) + "\n"
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	}
	if err != nil {
		log.Printf("Error opening browser: %v", err)
	}
}
