package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yoorquezt-labs/yqmev/internal/ai"
	"github.com/yoorquezt-labs/yqmev/internal/logging"
	"github.com/yoorquezt-labs/yqmev/internal/mcp"
	"github.com/yoorquezt-labs/yqmev/pkg/client"
)

// ---------------------------------------------------------------------------
// Colors — q8s-inspired palette
// ---------------------------------------------------------------------------

var (
	colorPrimary   = lipgloss.Color("#7C3AED") // purple
	colorSecondary = lipgloss.Color("#06B6D4") // cyan
	colorSuccess   = lipgloss.Color("#22C55E") // green
	colorWarning   = lipgloss.Color("#EAB308") // yellow
	colorDanger    = lipgloss.Color("#EF4444") // red
	colorMuted     = lipgloss.Color("#6B7280") // gray
	colorText      = lipgloss.Color("#E5E7EB") // light gray
	colorBgPanel   = lipgloss.Color("#1F2937") // panel bg
)

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).PaddingLeft(1)

	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSecondary).
			BorderBottom(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(colorMuted)

	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).
			Background(lipgloss.Color("#1E1B4B"))

	normalStyle = lipgloss.NewStyle().Foreground(colorText)

	mutedStyle = lipgloss.NewStyle().Foreground(colorMuted)

	statusHealthy  = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true)
	statusDegraded = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
	statusCritical = lipgloss.NewStyle().Foreground(colorDanger).Bold(true)

	panelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).Padding(0, 1)

	panelSelectedStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
				BorderForeground(colorPrimary).Padding(0, 1)

	panelHealthyStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
				BorderForeground(colorSuccess).Padding(0, 1)

	helpBarStyle = lipgloss.NewStyle().Foreground(colorMuted).
			Background(colorBgPanel).PaddingLeft(1)

	breadcrumbSep    = lipgloss.NewStyle().Foreground(colorMuted)
	breadcrumbItem   = lipgloss.NewStyle().Foreground(colorSecondary)
	breadcrumbActive = lipgloss.NewStyle().Foreground(colorText).Bold(true)
)

// ---------------------------------------------------------------------------
// Sparkline bar
// ---------------------------------------------------------------------------

func sparkBar(percent, width int) string {
	if width <= 0 {
		width = 10
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := width * percent / 100
	empty := width - filled

	var style lipgloss.Style
	switch {
	case percent >= 85:
		style = statusCritical
	case percent >= 60:
		style = statusDegraded
	default:
		style = statusHealthy
	}
	return style.Render(strings.Repeat("█", filled)) +
		mutedStyle.Render(strings.Repeat("░", empty))
}

// ---------------------------------------------------------------------------
// Views
// ---------------------------------------------------------------------------

type viewID int

const (
	viewSplash viewID = iota
	viewDashboard
	viewBundles
	viewBlocks
	viewBlockDetail
	viewBundleDetail
	viewRelays
	viewIntents
	viewSystem
	viewLogs
	viewOFA
	viewTrader
	viewTrading
	viewQAI
	viewModelPicker
)

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

type modelOption struct {
	Name         string
	Provider     string // claude, openai, ollama
	Model        string
	Intelligence int // 1-5
	Speed        int // 1-5
	Cost         int // 1-5 (0 = free)
}

var availableModels = []modelOption{
	{"Claude Opus 4.6", "claude", "claude-opus-4-6", 5, 2, 5},
	{"Claude Sonnet 4.6", "claude", "claude-sonnet-4-6", 4, 4, 3},
	{"Claude Haiku 4.5", "claude", "claude-haiku-4-5-20251001", 3, 5, 1},
	{"GPT-4o", "openai", "gpt-4o", 4, 4, 3},
	{"GPT-4.1", "openai", "gpt-4.1", 4, 3, 4},
	{"Llama 3.1", "ollama", "llama3.1", 3, 3, 0},
	{"Mistral", "ollama", "mistral", 3, 4, 0},
	{"DeepSeek R1", "ollama", "deepseek-r1", 4, 2, 0},
}

type dashboardData struct {
	healthy    bool
	poolSize   int
	blockCount int
	wsEvents   int
	topBid     string
	lastProfit string
	relayCount int
}

type bundleEntry struct {
	ID             string
	BidWei         string
	EffectiveValue string
	Simulated      string
	Reverted       string
	Timestamp      int64
	Chain          string
	TargetBlock    string
}

// bundleStats holds aggregated monitoring metrics for the bundle pool.
type bundleStats struct {
	totalBundles  int
	simulatedPct  float64
	revertedPct   float64
	avgBidWei     int64
	maxBidWei     int64
	minBidWei     int64
	totalValueWei int64
	bidHistory    []int64 // last 60 bid values for sparkline trend
	byBlock       map[string]int
}

type blockEntry struct {
	ID          string
	BundleCount int
	TotalProfit string
	Timestamp   int64
	CreatedAt   string
}

type ofaData struct {
	txsProtected    int
	sandwichBlocked int
	mevCaptured     string
	pendingRebates  int
	paidRebates     int
	recentEvents    []map[string]any
}

type traderSubmission struct {
	BundleID  string
	Status    string
	Chain     string
	BidWei    string
	Timestamp int64
}

// Trading types

type tradingSubTab int

const (
	tradingSubSwap tradingSubTab = iota
	tradingSubPositions
	tradingSubPortfolio
)

type positionEntry struct {
	Pair       string
	Chain      string
	Side       string // "long" / "short"
	EntryPrice string
	MarkPrice  string
	Size       string
	PnL        string
	PnLPct     string
	OpenedAt   int64
}

type portfolioToken struct {
	Symbol   string
	Chain    string
	Balance  string
	ValueUSD string
	PricePct string // 24h change
}

type swapResult struct {
	TxHash string
	Status string
}

// ---------------------------------------------------------------------------
// Tea messages
// ---------------------------------------------------------------------------

type tickMsg struct{}
type splashDoneMsg struct{}

type connectedMsg struct {
	c   *client.Client
	err error
}
type wsSubMsg struct{ ch <-chan json.RawMessage }
type wsEventMsg struct{ raw json.RawMessage }
type wsClosedMsg struct{}

type dashboardMsg struct {
	data dashboardData
	err  error
}
type bundlesMsg struct {
	bundles []bundleEntry
	err     error
}
type blocksMsg struct {
	blocks []blockEntry
	err    error
}
type blockDetailMsg struct {
	raw json.RawMessage
	err error
}
type relaysMsg struct {
	relays []map[string]any
	count  int
	err    error
}
type intentsMsg struct {
	solvers []map[string]any
	count   int
	err     error
}
type systemMsg struct {
	orderflow map[string]any
	cache     map[string]any
	relay     map[string]any
	err       error
}
type logsMsg struct {
	lines []string
	err   error
}
type filterInputMsg struct{ input string }
type ofaMsg struct {
	data ofaData
	err  error
}
type traderMsg struct {
	submissions []traderSubmission
	err         error
}
type traderSubmitResultMsg struct {
	bundleID string
	err      error
}
type tradingMsg struct {
	positions []positionEntry
	portfolio []portfolioToken
	err       error
}
type swapResultMsg struct {
	result swapResult
	err    error
}
type commandResultMsg struct {
	result string
	err    error
}
type aiTokenMsg struct{ token string }
type aiDoneMsg struct{ err error }
type mcpToolResultMsg struct {
	toolName string
	result   string
	err      error
}

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type model struct {
	cfg    client.Config
	client *client.Client

	// View state
	currentView viewID
	breadcrumb  []string
	width       int
	height      int
	ready       bool

	// Viewport for scrollable content
	viewport viewport.Model

	// Connection
	connected bool
	wsEvents  int
	wsCh      <-chan json.RawMessage

	// Dashboard summary
	dashboard dashboardData

	// Data
	bundles      []bundleEntry
	bundleStats  bundleStats
	bundlePaused bool
	bundleSort   int // 0=value-desc, 1=time-desc, 2=id
	bundleAlertThreshold int64 // alert when bid exceeds this (wei)
	blocks      []blockEntry
	blockDetail json.RawMessage
	relays      []map[string]any
	relayCount  int
	solvers     []map[string]any
	solverCount int
	systemOF    map[string]any
	systemCache map[string]any
	systemRelay map[string]any

	// Cursors
	dashCursor   int
	bundleCursor int
	blockCursor  int
	relayCursor  int
	solverCursor int

	// Filter
	filterActive bool
	filterInput  textinput.Model
	filterText   string

	// Animation
	spinner spinner.Model
	frame   int

	// OFA
	ofa ofaData

	// Trader
	traderSubmissions  []traderSubmission
	traderFieldCursor  int // 0=bid, 1=chain, 2=txjson, 3=target
	traderBidInput     textinput.Model
	traderChainInput   textinput.Model
	traderTxInput      textinput.Model
	traderTargetInput  textinput.Model
	traderAuctionEvts  []string

	// Trading
	tradingSubTab      tradingSubTab
	tradingPositions   []positionEntry
	tradingPortfolio   []portfolioToken
	tradingPosCursor   int
	tradingPortCursor  int
	tradingSwapField   int // 0=tokenIn, 1=tokenOut, 2=amount, 3=chain, 4=slippage
	tradingTokenInInput   textinput.Model
	tradingTokenOutInput  textinput.Model
	tradingAmountInput    textinput.Model
	tradingSwapChainInput textinput.Model
	tradingSlippageInput  textinput.Model
	tradingLastSwap       *swapResult

	// Command mode
	commandMode       bool
	commandInput      textinput.Model
	lastCommandResult string
	lastCommandError  bool

	// Model picker
	modelPickerCursor int

	// Q AI
	aiProvider   ai.Provider
	aiInput      textinput.Model
	aiMessages   []ai.Message
	aiStreaming   bool
	aiStreamBuf  string
	aiQState     int // 0=idle,1=listening,2=thinking,3=talking,4=success,5=error
	aiTokenCh    chan string
	aiErrCh      chan error

	// MCP
	mcpClient    *mcp.Client
	mcpAvailable bool

	// Misc
	err       error
	lastFetch time.Time
}

func initialModel(cfg client.Config) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(colorPrimary)

	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.CharLimit = 100
	ti.Width = 40

	ci := textinput.New()
	ci.Placeholder = ":command..."
	ci.CharLimit = 256
	ci.Width = 60

	bidIn := textinput.New()
	bidIn.Placeholder = "1000000000"
	bidIn.CharLimit = 40
	bidIn.Width = 30

	chainIn := textinput.New()
	chainIn.Placeholder = "ethereum"
	chainIn.CharLimit = 20
	chainIn.Width = 20

	txIn := textinput.New()
	txIn.Placeholder = `["0x...signed_tx"]`
	txIn.CharLimit = 1024
	txIn.Width = 50

	targetIn := textinput.New()
	targetIn.Placeholder = "0 (next block)"
	targetIn.CharLimit = 12
	targetIn.Width = 16

	aiIn := textinput.New()
	aiIn.Placeholder = "Ask Q about MEV activity..."
	aiIn.CharLimit = 500
	aiIn.Width = 60

	tokenInIn := textinput.New()
	tokenInIn.Placeholder = "WETH"
	tokenInIn.CharLimit = 20
	tokenInIn.Width = 20

	tokenOutIn := textinput.New()
	tokenOutIn.Placeholder = "USDC"
	tokenOutIn.CharLimit = 20
	tokenOutIn.Width = 20

	amountIn := textinput.New()
	amountIn.Placeholder = "1.0"
	amountIn.CharLimit = 30
	amountIn.Width = 24

	swapChainIn := textinput.New()
	swapChainIn.Placeholder = "ethereum"
	swapChainIn.CharLimit = 20
	swapChainIn.Width = 20

	slippageIn := textinput.New()
	slippageIn.Placeholder = "0.5"
	slippageIn.CharLimit = 6
	slippageIn.Width = 10

	return model{
		cfg:              cfg,
		currentView:      viewSplash,
		breadcrumb:       []string{"dashboard"},
		spinner:          s,
		filterInput:      ti,
		commandInput:     ci,
		traderBidInput:   bidIn,
		traderChainInput: chainIn,
		traderTxInput:    txIn,
		traderTargetInput: targetIn,
		aiInput:          aiIn,
		tradingTokenInInput:   tokenInIn,
		tradingTokenOutInput:  tokenOutIn,
		tradingAmountInput:    amountIn,
		tradingSwapChainInput: swapChainIn,
		tradingSlippageInput:  slippageIn,
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		connectCmd(m.cfg),
		tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} }),
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return splashDoneMsg{} }),
	)
}

func connectCmd(cfg client.Config) tea.Cmd {
	return func() tea.Msg {
		c, err := client.Dial(cfg)
		return connectedMsg{c: c, err: err}
	}
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg{} })
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpH := m.height - 10
		if vpH < 5 {
			vpH = 5
		}
		if !m.ready {
			m.viewport = viewport.New(m.width, vpH)
			m.ready = true
		} else {
			m.viewport.Width = m.width
			m.viewport.Height = vpH
		}

	case tea.KeyMsg:
		// Splash — skip on any key
		if m.currentView == viewSplash {
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			m.currentView = viewDashboard
			m.breadcrumb = []string{"dashboard"}
			cmds = append(cmds, m.fetchDashboard())
			return m, tea.Batch(cmds...)
		}

		// Command mode
		if m.commandMode {
			return m.updateCommand(msg)
		}

		// Model picker
		if m.currentView == viewModelPicker {
			switch msg.String() {
			case "q", "ctrl+c":
				if m.client != nil {
					m.client.Close()
				}
				return m, tea.Quit
			case "esc":
				m.currentView = viewQAI
				m.aiInput.Focus()
				return m, textinput.Blink
			case "j", "down":
				if m.modelPickerCursor < len(availableModels)-1 {
					m.modelPickerCursor++
				}
				return m, nil
			case "k", "up":
				if m.modelPickerCursor > 0 {
					m.modelPickerCursor--
				}
				return m, nil
			case "enter":
				selected := availableModels[m.modelPickerCursor]
				switch selected.Provider {
				case "claude":
					key := envOr("ANTHROPIC_API_KEY", envOr("YQMEV_AI_KEY", ""))
					if key != "" {
						m.aiProvider = ai.NewClaude(key, selected.Model)
					} else {
						m.err = fmt.Errorf("ANTHROPIC_API_KEY not set")
					}
				case "openai":
					key := envOr("OPENAI_API_KEY", envOr("YQMEV_AI_KEY", ""))
					if key != "" {
						m.aiProvider = ai.NewOpenAI(key, selected.Model, "")
					} else {
						m.err = fmt.Errorf("OPENAI_API_KEY not set")
					}
				case "ollama":
					m.aiProvider = ai.NewOllama(selected.Model, envOr("OLLAMA_URL", ""))
				}
				m.currentView = viewQAI
				m.aiInput.Focus()
				return m, textinput.Blink
			}
			return m, nil
		}

		// Q AI input mode
		if m.currentView == viewQAI && !m.commandMode {
			switch msg.String() {
			case "q", "ctrl+c":
				if m.client != nil {
					m.client.Close()
				}
				return m, tea.Quit
			case "esc":
				if m.aiStreaming {
					return m, nil // can't leave while streaming
				}
				return m.goBack()
			case "enter":
				return m.handleAISend()
			case "m":
				if !m.aiStreaming && m.aiInput.Value() == "" {
					m.currentView = viewModelPicker
					m.modelPickerCursor = 0
					// Pre-select current model
					if m.aiProvider != nil {
						for i, mo := range availableModels {
							if mo.Name == m.aiProvider.Name() || mo.Model == m.aiProvider.Name() {
								m.modelPickerCursor = i
								break
							}
						}
					}
					return m, nil
				}
				var cmd tea.Cmd
				m.aiInput, cmd = m.aiInput.Update(msg)
				if m.aiInput.Value() != "" {
					m.aiQState = 1
				} else {
					m.aiQState = 0
				}
				return m, cmd
			default:
				var cmd tea.Cmd
				m.aiInput, cmd = m.aiInput.Update(msg)
				if m.aiInput.Value() != "" {
					m.aiQState = 1 // listening
				} else {
					m.aiQState = 0
				}
				return m, cmd
			}
		}

		// Trading view input mode
		if m.currentView == viewTrading {
			switch msg.String() {
			case "q", "ctrl+c":
				if m.client != nil {
					m.client.Close()
				}
				return m, tea.Quit
			case "esc":
				return m.goBack()
			case "1":
				m.tradingSubTab = tradingSubSwap
				m.tradingSwapField = 0
				m.focusTradingField()
				return m, textinput.Blink
			case "2":
				m.tradingSubTab = tradingSubPositions
				m.blurTradingFields()
				return m, nil
			case "3":
				m.tradingSubTab = tradingSubPortfolio
				m.blurTradingFields()
				return m, nil
			case "tab":
				if m.tradingSubTab == tradingSubSwap {
					m.tradingSwapField = (m.tradingSwapField + 1) % 5
					m.focusTradingField()
					return m, textinput.Blink
				}
			case "shift+tab":
				if m.tradingSubTab == tradingSubSwap {
					m.tradingSwapField = (m.tradingSwapField + 4) % 5
					m.focusTradingField()
					return m, textinput.Blink
				}
			case "j", "down":
				switch m.tradingSubTab {
				case tradingSubPositions:
					if m.tradingPosCursor < len(m.tradingPositions)-1 {
						m.tradingPosCursor++
					}
				case tradingSubPortfolio:
					if m.tradingPortCursor < len(m.tradingPortfolio)-1 {
						m.tradingPortCursor++
					}
				}
				return m, nil
			case "k", "up":
				switch m.tradingSubTab {
				case tradingSubPositions:
					if m.tradingPosCursor > 0 {
						m.tradingPosCursor--
					}
				case tradingSubPortfolio:
					if m.tradingPortCursor > 0 {
						m.tradingPortCursor--
					}
				}
				return m, nil
			case "enter":
				if m.tradingSubTab == tradingSubSwap {
					return m.handleSwapSubmit()
				}
				return m, nil
			default:
				if m.tradingSubTab == tradingSubSwap {
					return m.updateTradingInput(msg)
				}
			}
			return m, nil
		}

		// Trader form input mode
		if m.currentView == viewTrader && m.traderFieldCursor >= 0 {
			switch msg.String() {
			case "q", "ctrl+c":
				if m.client != nil {
					m.client.Close()
				}
				return m, tea.Quit
			case "esc":
				return m.goBack()
			case "tab":
				m.traderFieldCursor = (m.traderFieldCursor + 1) % 4
				m.focusTraderField()
				return m, textinput.Blink
			case "shift+tab":
				m.traderFieldCursor = (m.traderFieldCursor + 3) % 4
				m.focusTraderField()
				return m, textinput.Blink
			case "enter":
				return m.handleTraderSubmit()
			default:
				return m.updateTraderInput(msg)
			}
		}

		// Filter mode
		if m.filterActive {
			return m.updateFilter(msg)
		}

		switch msg.String() {
		case "q", "ctrl+c":
			if m.client != nil {
				m.client.Close()
			}
			return m, tea.Quit

		case "esc":
			return m.goBack()

		case "/":
			m.filterActive = true
			m.filterInput.SetValue(m.filterText)
			m.filterInput.Focus()
			return m, textinput.Blink

		case "r":
			cmds = append(cmds, m.fetchCurrentView())

		case " ":
			if m.currentView == viewBundles {
				m.bundlePaused = !m.bundlePaused
			}

		case "s":
			if m.currentView == viewBundles {
				m.bundleSort = (m.bundleSort + 1) % 3
			}

		// Vim navigation
		case "j", "down":
			m.cursorDown()
		case "k", "up":
			m.cursorUp()

		case "enter":
			return m.handleEnter()

		// Quick-jump
		case "1":
			return m.navigateTo(viewDashboard, "dashboard")
		case "2":
			return m.navigateTo(viewBundles, "bundles")
		case "3":
			return m.navigateTo(viewBlocks, "blocks")
		case "4":
			return m.navigateTo(viewRelays, "relays")
		case "5":
			return m.navigateTo(viewIntents, "intents")
		case "6":
			return m.navigateTo(viewSystem, "system")
		case "7":
			return m.navigateTo(viewOFA, "ofa")
		case "8":
			m.currentView = viewTrader
			m.breadcrumb = []string{"trader"}
			m.traderFieldCursor = 0
			m.focusTraderField()
			cmds = append(cmds, m.fetchTrader(), textinput.Blink)
			return m, tea.Batch(cmds...)
		case "9":
			m.currentView = viewTrading
			m.breadcrumb = []string{"trading"}
			m.tradingSubTab = tradingSubSwap
			m.tradingSwapField = 0
			m.focusTradingField()
			cmds = append(cmds, m.fetchTrading(), textinput.Blink)
			return m, tea.Batch(cmds...)
		case "0":
			m.currentView = viewQAI
			m.breadcrumb = []string{"Q AI"}
			m.aiInput.Focus()
			m.aiQState = 0
			return m, textinput.Blink

		case ":":
			m.commandMode = true
			m.commandInput.SetValue("")
			m.commandInput.Focus()
			return m, textinput.Blink
		}

	case connectedMsg:
		if msg.err != nil {
			m.connected = false
			m.err = fmt.Errorf("connect: %v", msg.err)
			logging.Error("gateway connection failed", "error", msg.err)
		} else {
			m.client = msg.c
			m.connected = true
			m.err = nil
			logging.Info("gateway connected", "url", m.cfg.GatewayURL)
			cmds = append(cmds, m.fetchDashboard(), subscribeCmd(m.client))
		}

	case splashDoneMsg:
		if m.currentView == viewSplash {
			m.currentView = viewDashboard
			m.breadcrumb = []string{"dashboard"}
			cmds = append(cmds, m.fetchDashboard())
		}

	case tickMsg:
		m.frame++
		cmds = append(cmds, tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} }))
		// Auto-refresh every 5s
		if m.connected && m.frame%20 == 0 {
			cmds = append(cmds, m.fetchCurrentView())
		}
		if !m.connected && m.frame%20 == 0 {
			cmds = append(cmds, connectCmd(m.cfg))
		}

	case wsSubMsg:
		m.wsCh = msg.ch
		cmds = append(cmds, readWSCmd(m.wsCh))
	case wsEventMsg:
		m.wsEvents++
		cmds = append(cmds, readWSCmd(m.wsCh))
		if m.currentView == viewDashboard || m.currentView == viewTrader || m.currentView == viewTrading {
			cmds = append(cmds, m.fetchCurrentView())
		}
		if m.currentView == viewBundles && !m.bundlePaused {
			cmds = append(cmds, m.fetchCurrentView())
		}
		if m.currentView == viewTrader {
			// Append auction event to the watcher
			m.traderAuctionEvts = append(m.traderAuctionEvts, string(msg.raw))
			if len(m.traderAuctionEvts) > 50 {
				m.traderAuctionEvts = m.traderAuctionEvts[len(m.traderAuctionEvts)-50:]
			}
		}
	case wsClosedMsg:
		m.wsCh = nil
		logging.Warn("websocket closed")

	// Data messages
	case dashboardMsg:
		if msg.err == nil {
			m.dashboard = msg.data
			m.lastFetch = time.Now()
			m.err = nil
		} else {
			m.err = msg.err
		}
	case bundlesMsg:
		if msg.err == nil {
			m.bundles = msg.bundles
			m.bundleStats = computeBundleStats(msg.bundles, m.bundleStats.bidHistory)
			m.lastFetch = time.Now()
			m.err = nil
			// Bell alert for high-value bundles
			if m.bundleAlertThreshold > 0 && m.bundleStats.maxBidWei >= m.bundleAlertThreshold {
				fmt.Print("\a") // terminal bell
			}
		} else {
			m.err = msg.err
		}
	case blocksMsg:
		if msg.err == nil {
			m.blocks = msg.blocks
			m.lastFetch = time.Now()
			m.err = nil
		} else {
			m.err = msg.err
		}
	case blockDetailMsg:
		if msg.err == nil {
			m.blockDetail = msg.raw
			m.lastFetch = time.Now()
			m.err = nil
		} else {
			m.err = msg.err
		}
	case relaysMsg:
		if msg.err == nil {
			m.relays = msg.relays
			m.relayCount = msg.count
			m.lastFetch = time.Now()
			m.err = nil
		} else if !strings.Contains(msg.err.Error(), "404") {
			m.err = msg.err
		}
	case intentsMsg:
		if msg.err == nil {
			m.solvers = msg.solvers
			m.solverCount = msg.count
			m.lastFetch = time.Now()
			m.err = nil
		} else if !strings.Contains(msg.err.Error(), "404") {
			m.err = msg.err
		}
	case systemMsg:
		if msg.err == nil {
			m.systemOF = msg.orderflow
			m.systemCache = msg.cache
			m.systemRelay = msg.relay
			m.lastFetch = time.Now()
			m.err = nil
		} else if !strings.Contains(msg.err.Error(), "404") {
			m.err = msg.err
		}

	case ofaMsg:
		if msg.err == nil {
			m.ofa = msg.data
			m.lastFetch = time.Now()
			m.err = nil
		} else if !strings.Contains(msg.err.Error(), "404") {
			m.err = msg.err
		}
	case tradingMsg:
		if msg.err == nil {
			m.tradingPositions = msg.positions
			m.tradingPortfolio = msg.portfolio
			m.lastFetch = time.Now()
			m.err = nil
		} else if !strings.Contains(msg.err.Error(), "404") {
			m.err = msg.err
		}
	case swapResultMsg:
		if msg.err == nil {
			m.tradingLastSwap = &msg.result
			m.lastCommandResult = fmt.Sprintf("Swap submitted: %s", msg.result.TxHash)
			m.lastCommandError = false
			logging.Info("swap executed", "tx_hash", msg.result.TxHash, "status", msg.result.Status)
		} else {
			m.lastCommandResult = fmt.Sprintf("Swap failed: %s", msg.err)
			m.lastCommandError = true
			logging.Error("swap failed", "error", msg.err)
		}
		cmds = append(cmds, m.fetchTrading())
	case traderMsg:
		if msg.err == nil {
			m.traderSubmissions = msg.submissions
			m.lastFetch = time.Now()
			m.err = nil
		} else if !strings.Contains(msg.err.Error(), "404") {
			m.err = msg.err
		}
	case traderSubmitResultMsg:
		if msg.err == nil {
			m.lastCommandResult = fmt.Sprintf("Bundle submitted: %s", msg.bundleID)
			m.lastCommandError = false
		} else {
			m.lastCommandResult = fmt.Sprintf("Submit failed: %s", msg.err)
			m.lastCommandError = true
		}
		cmds = append(cmds, m.fetchTrader())
	case commandResultMsg:
		if msg.err == nil {
			m.lastCommandResult = msg.result
			m.lastCommandError = false
		} else {
			m.lastCommandResult = msg.err.Error()
			m.lastCommandError = true
		}

	case aiTokenMsg:
		m.aiStreamBuf += msg.token
		m.aiQState = 3 // talking
		cmds = append(cmds, waitForToken(m.aiTokenCh, m.aiErrCh))
	case aiDoneMsg:
		m.aiStreaming = false
		if msg.err != nil {
			m.aiMessages = append(m.aiMessages, ai.Message{Role: "assistant", Content: "Error: " + msg.err.Error()})
			m.aiQState = 5 // error
			logging.Error("ai stream error", "error", msg.err)
		} else {
			logging.Debug("ai stream complete", "length", len(m.aiStreamBuf))
			// Check if the AI wants to call an MCP tool
			if tc := ai.ParseToolCall(m.aiStreamBuf); tc != nil && m.mcpAvailable {
				// Show the AI's reasoning before the tool call
				m.aiMessages = append(m.aiMessages, ai.Message{Role: "assistant", Content: m.aiStreamBuf})
				m.aiQState = 2 // thinking (waiting for tool)
				m.aiStreaming = true
				m.aiStreamBuf = ""
				mcpC := m.mcpClient
				toolName := tc.Name
				toolArgs := tc.Args
				logging.Info("mcp tool call", "tool", toolName)
				cmds = append(cmds, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					result, err := mcpC.CallTool(ctx, toolName, json.RawMessage(toolArgs))
					if err != nil {
						logging.Error("mcp tool failed", "tool", toolName, "error", err)
					} else {
						logging.Debug("mcp tool result", "tool", toolName, "length", len(result))
					}
					return mcpToolResultMsg{toolName: toolName, result: result, err: err}
				})
			} else {
				m.aiMessages = append(m.aiMessages, ai.Message{Role: "assistant", Content: m.aiStreamBuf})
				m.aiQState = 4 // success
			}
		}
		m.aiStreamBuf = ""

	case mcpToolResultMsg:
		// Tool result received — feed it back to the AI as context
		var toolContent string
		if msg.err != nil {
			toolContent = fmt.Sprintf("[Tool %s failed: %s]", msg.toolName, msg.err.Error())
		} else {
			toolContent = fmt.Sprintf("[Tool %s result]\n%s", msg.toolName, msg.result)
		}
		// Add tool result as a user message (context injection)
		m.aiMessages = append(m.aiMessages, ai.Message{Role: "user", Content: toolContent})
		// Re-run AI with the tool result so it can summarize
		m.aiStreamBuf = ""
		m.aiTokenCh = make(chan string, 64)
		m.aiErrCh = make(chan error, 1)
		tokenCh := m.aiTokenCh
		errCh := m.aiErrCh
		provider := m.aiProvider
		history := make([]ai.Message, len(m.aiMessages))
		copy(history, m.aiMessages)
		mevCtx := m.buildMEVContext()
		cmds = append(cmds, func() tea.Msg {
			req := ai.AnalyzeRequest{
				Question:   "Based on the tool result above, provide a concise summary for the user.",
				MEVContext: mevCtx,
				History:    history,
			}
			go func() {
				errCh <- provider.Stream(context.Background(), req, tokenCh)
			}()
			return waitForToken(tokenCh, errCh)()
		})

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// ---------------------------------------------------------------------------
// Navigation
// ---------------------------------------------------------------------------

func (m model) navigateTo(v viewID, label string) (model, tea.Cmd) {
	m.currentView = v
	m.breadcrumb = []string{label}
	m.filterText = ""
	return m, m.fetchCurrentView()
}

func (m model) goBack() (model, tea.Cmd) {
	switch m.currentView {
	case viewBlockDetail:
		m.currentView = viewBlocks
		m.breadcrumb = []string{"blocks"}
	case viewBundleDetail:
		m.currentView = viewBundles
		m.breadcrumb = []string{"bundles"}
	default:
		m.currentView = viewDashboard
		m.breadcrumb = []string{"dashboard"}
	}
	return m, m.fetchCurrentView()
}

const dashCardCount = 9

func (m *model) cursorDown() {
	switch m.currentView {
	case viewDashboard:
		if m.dashCursor < dashCardCount-1 {
			m.dashCursor++
		}
	case viewBundles:
		if m.bundleCursor < len(m.bundles)-1 {
			m.bundleCursor++
		}
	case viewBlocks:
		if m.blockCursor < len(m.blocks)-1 {
			m.blockCursor++
		}
	case viewRelays:
		if m.relayCursor < len(m.relays)-1 {
			m.relayCursor++
		}
	case viewIntents:
		if m.solverCursor < len(m.solvers)-1 {
			m.solverCursor++
		}
	}
}

func (m *model) cursorUp() {
	switch m.currentView {
	case viewDashboard:
		if m.dashCursor > 0 {
			m.dashCursor--
		}
	case viewBundles:
		if m.bundleCursor > 0 {
			m.bundleCursor--
		}
	case viewBlocks:
		if m.blockCursor > 0 {
			m.blockCursor--
		}
	case viewRelays:
		if m.relayCursor > 0 {
			m.relayCursor--
		}
	case viewIntents:
		if m.solverCursor > 0 {
			m.solverCursor--
		}
	}
}

// dashTargets maps dashboard card index to (viewID, label).
var dashTargets = []struct {
	view  viewID
	label string
}{
	{viewBundles, "bundles"},
	{viewBlocks, "blocks"},
	{viewRelays, "relays"},
	{viewIntents, "intents"},
	{viewSystem, "system"},
	{viewOFA, "ofa"},
	{viewTrader, "trader"},
	{viewTrading, "trading"},
	{viewQAI, "Q AI"},
}

func (m model) handleEnter() (model, tea.Cmd) {
	switch m.currentView {
	case viewDashboard:
		if m.dashCursor >= 0 && m.dashCursor < len(dashTargets) {
			t := dashTargets[m.dashCursor]
			m.currentView = t.view
			m.breadcrumb = []string{t.label}
			return m, m.fetchCurrentView()
		}
	case viewBlocks:
		if m.blockCursor < len(m.blocks) {
			block := m.blocks[m.blockCursor]
			m.currentView = viewBlockDetail
			m.breadcrumb = []string{"blocks", trunc(block.ID, 12)}
			return m, m.fetchBlockDetail(block.ID)
		}
	case viewBundles:
		if m.bundleCursor < len(m.bundles) {
			m.currentView = viewBundleDetail
			b := m.bundles[m.bundleCursor]
			m.breadcrumb = []string{"bundles", trunc(b.ID, 16)}
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Filter
// ---------------------------------------------------------------------------

func (m model) updateFilter(msg tea.KeyMsg) (model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filterText = m.filterInput.Value()
		m.filterActive = false
		m.filterInput.Blur()
		return m, nil
	case "esc":
		m.filterActive = false
		m.filterInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	return m, cmd
}

// ---------------------------------------------------------------------------
// Data-fetching commands
// ---------------------------------------------------------------------------

func (m model) fetchCurrentView() tea.Cmd {
	switch m.currentView {
	case viewDashboard:
		return m.fetchDashboard()
	case viewBundles:
		return m.fetchBundles()
	case viewBlocks:
		return m.fetchBlocks()
	case viewRelays:
		return m.fetchRelays()
	case viewIntents:
		return m.fetchIntents()
	case viewSystem:
		return m.fetchSystem()
	case viewOFA:
		return m.fetchOFA()
	case viewTrader:
		return m.fetchTrader()
	case viewTrading:
		return m.fetchTrading()
	}
	return nil
}

func (m model) fetchDashboard() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		if c == nil {
			return dashboardMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		d := dashboardData{}

		// Health
		if _, err := c.Health(ctx); err == nil {
			d.healthy = true
		}

		// Auction
		raw, err := c.GetAuction(ctx)
		if err == nil {
			var resp struct {
				PoolSize int                      `json:"pool_size"`
				Bundles  []map[string]interface{} `json:"bundles"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				d.poolSize = resp.PoolSize
				if len(resp.Bundles) > 0 {
					d.topBid = jstr(resp.Bundles[0], "effective_value")
				}
			}
		}

		// Blocks
		raw, err = c.ListBlocks(ctx)
		if err == nil {
			var resp struct {
				Count  int                      `json:"count"`
				Blocks []map[string]interface{} `json:"blocks"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				d.blockCount = resp.Count
				if len(resp.Blocks) > 0 {
					d.lastProfit = jstr(resp.Blocks[0], "total_profit")
				}
			}
		}

		return dashboardMsg{data: d}
	}
}

func (m model) fetchBundles() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		if c == nil {
			return bundlesMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		raw, err := c.GetAuction(ctx)
		if err != nil {
			return bundlesMsg{err: err}
		}
		var resp struct {
			Bundles []map[string]interface{} `json:"bundles"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return bundlesMsg{err: err}
		}
		var bundles []bundleEntry
		for _, b := range resp.Bundles {
			bundles = append(bundles, bundleEntry{
				ID:             jstr(b, "bundle_id"),
				BidWei:         jstr(b, "bid_wei"),
				EffectiveValue: jstr(b, "effective_value"),
				Simulated:      jstr(b, "simulated"),
				Reverted:       jstr(b, "reverted"),
				Timestamp:      jint(b, "timestamp"),
				Chain:          jstr(b, "chain"),
			})
		}
		return bundlesMsg{bundles: bundles}
	}
}

func (m model) fetchBlocks() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		if c == nil {
			return blocksMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		raw, err := c.ListBlocks(ctx)
		if err != nil {
			return blocksMsg{err: err}
		}
		var resp struct {
			Blocks []map[string]interface{} `json:"blocks"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return blocksMsg{err: err}
		}
		var blocks []blockEntry
		for _, b := range resp.Blocks {
			blocks = append(blocks, blockEntry{
				ID:          jstr(b, "block_id"),
				BundleCount: int(jfloat(b, "bundle_count")),
				TotalProfit: jstr(b, "total_profit"),
				Timestamp:   jint(b, "timestamp"),
				CreatedAt:   jstr(b, "created_at"),
			})
		}
		return blocksMsg{blocks: blocks}
	}
}

func (m model) fetchBlockDetail(id string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		if c == nil {
			return blockDetailMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		raw, err := c.Call(ctx, "mev_getBlock", map[string]any{"id": id})
		if err != nil {
			return blockDetailMsg{err: err}
		}
		return blockDetailMsg{raw: raw}
	}
}

func (m model) fetchRelays() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		if c == nil {
			return relaysMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		raw, err := c.RelayList(ctx)
		if err != nil {
			return relaysMsg{err: err}
		}
		var resp struct {
			Count  int              `json:"count"`
			Relays []map[string]any `json:"relays"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return relaysMsg{err: err}
		}
		return relaysMsg{relays: resp.Relays, count: resp.Count}
	}
}

func (m model) fetchIntents() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		if c == nil {
			return intentsMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		raw, err := c.Call(ctx, "mev_listSolvers", nil)
		if err != nil {
			return intentsMsg{err: err}
		}
		var resp struct {
			Count   int              `json:"count"`
			Solvers []map[string]any `json:"solvers"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return intentsMsg{err: err}
		}
		return intentsMsg{solvers: resp.Solvers, count: resp.Count}
	}
}

func (m model) fetchSystem() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		if c == nil {
			return systemMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		sd := systemMsg{}
		raw, err := c.OrderflowSummary(ctx)
		if err == nil {
			var v map[string]any
			if json.Unmarshal(raw, &v) == nil {
				sd.orderflow = v
			}
		}
		raw, err = c.Call(ctx, "mev_simCacheStats", nil)
		if err == nil {
			var v map[string]any
			if json.Unmarshal(raw, &v) == nil {
				sd.cache = v
			}
		}
		raw, err = c.RelayStats(ctx)
		if err == nil {
			var v map[string]any
			if json.Unmarshal(raw, &v) == nil {
				sd.relay = v
			}
		}
		return sd
	}
}

func (m model) fetchOFA() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		if c == nil {
			return ofaMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		od := ofaData{}

		// Stats
		raw, err := c.Call(ctx, "yq_getStats", nil)
		if err == nil {
			var stats map[string]any
			if json.Unmarshal(raw, &stats) == nil {
				od.txsProtected = int(jfloat(stats, "txs_protected"))
				od.sandwichBlocked = int(jfloat(stats, "sandwiches_blocked"))
				od.mevCaptured = jstr(stats, "mev_captured")
			}
		}

		// Pending rebates
		raw, err = c.Call(ctx, "ofa_getPendingRebates", nil)
		if err == nil {
			var rebates map[string]any
			if json.Unmarshal(raw, &rebates) == nil {
				od.pendingRebates = int(jfloat(rebates, "pending"))
				od.paidRebates = int(jfloat(rebates, "paid"))
			}
		}

		// Recent events from protect status
		raw, err = c.Call(ctx, "yq_getRebateHistory", nil)
		if err == nil {
			var resp struct {
				Events []map[string]any `json:"events"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				od.recentEvents = resp.Events
				if len(od.recentEvents) > 10 {
					od.recentEvents = od.recentEvents[:10]
				}
			}
		}

		return ofaMsg{data: od}
	}
}

func (m model) fetchTrader() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		if c == nil {
			return traderMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		raw, err := c.GetAuction(ctx)
		if err != nil {
			return traderMsg{err: err}
		}
		var resp struct {
			Bundles []map[string]any `json:"bundles"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return traderMsg{err: err}
		}
		var subs []traderSubmission
		for _, b := range resp.Bundles {
			subs = append(subs, traderSubmission{
				BundleID:  jstr(b, "bundle_id"),
				Status:    jstr(b, "simulated"),
				Chain:     jstr(b, "chain"),
				BidWei:    jstr(b, "bid_wei"),
				Timestamp: jint(b, "timestamp"),
			})
		}
		return traderMsg{submissions: subs}
	}
}

func (m model) fetchTrading() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		if c == nil {
			return tradingMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		td := tradingMsg{}

		// Positions
		raw, err := c.Call(ctx, "trade_positions", nil)
		if err == nil {
			var resp struct {
				Positions []map[string]any `json:"positions"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				for _, p := range resp.Positions {
					td.positions = append(td.positions, positionEntry{
						Pair:       jstr(p, "pair"),
						Chain:      jstr(p, "chain"),
						Side:       jstr(p, "side"),
						EntryPrice: jstr(p, "entry_price"),
						MarkPrice:  jstr(p, "mark_price"),
						Size:       jstr(p, "size"),
						PnL:        jstr(p, "pnl"),
						PnLPct:     jstr(p, "pnl_pct"),
						OpenedAt:   jint(p, "opened_at"),
					})
				}
			}
		}

		// Portfolio
		raw, err = c.Call(ctx, "trade_portfolio", nil)
		if err == nil {
			var resp struct {
				Tokens []map[string]any `json:"tokens"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				for _, t := range resp.Tokens {
					td.portfolio = append(td.portfolio, portfolioToken{
						Symbol:   jstr(t, "symbol"),
						Chain:    jstr(t, "chain"),
						Balance:  jstr(t, "balance"),
						ValueUSD: jstr(t, "value_usd"),
						PricePct: jstr(t, "price_pct_24h"),
					})
				}
			}
		}

		return td
	}
}

// WebSocket commands

func subscribeCmd(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		if c == nil {
			return wsClosedMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, ch, err := c.Subscribe(ctx, "auction")
		if err != nil {
			return wsClosedMsg{}
		}
		return wsSubMsg{ch: ch}
	}
}

func readWSCmd(ch <-chan json.RawMessage) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return wsClosedMsg{}
		}
		raw, ok := <-ch
		if !ok {
			return wsClosedMsg{}
		}
		return wsEventMsg{raw: raw}
	}
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m model) View() string {
	if m.width == 0 {
		return m.spinner.View() + " Initializing..."
	}

	// Splash screen
	if m.currentView == viewSplash {
		return m.viewSplash()
	}

	var sb strings.Builder

	// Header panel
	sb.WriteString(m.viewHeader())
	sb.WriteString("\n")

	// Main content
	content := m.viewContent()
	if m.ready {
		m.viewport.SetContent(content)
		sb.WriteString(m.viewport.View())
	} else {
		sb.WriteString(content)
	}

	// Filter input
	if m.filterActive {
		sb.WriteString("\n")
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(colorSecondary).Padding(0, 1).
			Render("/ " + m.filterInput.View())
		sb.WriteString(box)
	}

	// Command input
	if m.commandMode {
		sb.WriteString("\n")
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).Padding(0, 1).
			Render(": " + m.commandInput.View())
		sb.WriteString(box)
	}

	// Command result
	if m.lastCommandResult != "" && !m.commandMode {
		sb.WriteString("\n")
		resultStyle := lipgloss.NewStyle().Foreground(colorSuccess).PaddingLeft(2)
		if m.lastCommandError {
			resultStyle = lipgloss.NewStyle().Foreground(colorDanger).PaddingLeft(2)
		}
		sb.WriteString(resultStyle.Render(m.lastCommandResult))
	}

	// Error
	if m.err != nil {
		sb.WriteString("\n")
		sb.WriteString(statusCritical.Render(fmt.Sprintf("  error: %s", m.err)))
	}

	// Help bar
	sb.WriteString("\n")
	sb.WriteString(m.viewHelpBar())

	return sb.String()
}

// ---------------------------------------------------------------------------
// Splash
// ---------------------------------------------------------------------------

func (m model) viewSplash() string {
	G := lipgloss.NewStyle().Foreground(colorPrimary).Render
	C := lipgloss.NewStyle().Foreground(colorSecondary).Render

	stars := []string{"✦", "✧", "·", "✧"}
	s1 := stars[m.frame%4]
	s2 := stars[(m.frame+2)%4]

	banner := []string{
		G("          ██    ██") + " " + G("██████") + "  " + C("███    ███") + " " + C("███████") + " " + C("██    ██"),
		G("           ██  ██") + "  " + G("██  ██") + " " + C("████  ████") + " " + C("██") + "      " + C("██    ██"),
		G("            ████") + "   " + G("██  ██") + " " + C("██ ████ ██") + " " + C("█████") + "   " + C("██    ██"),
		G("             ██") + "    " + G("██  ██") + " " + C("██  ██  ██") + " " + C("██") + "       " + C("██  ██"),
		G("             ██") + "     " + G("██████") + " " + C("██      ██") + " " + C("███████") + "   " + C("████"),
	}

	var sb strings.Builder
	sb.WriteString("\n\n")
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorPrimary).Render(s1) + "\n")
	for _, line := range banner {
		sb.WriteString("  " + line + "\n")
	}
	sb.WriteString(strings.Repeat(" ", 50) + lipgloss.NewStyle().Foreground(colorSecondary).Render(s2) + "\n\n")

	// Info panel
	lbl := lipgloss.NewStyle().Foreground(colorSuccess).Bold(true)
	val := lipgloss.NewStyle().Foreground(colorText)
	dim := mutedStyle

	sb.WriteString("  " + lbl.Render("Getting started") + "\n")
	sb.WriteString("  " + val.Render("Navigate with ") + lbl.Render("j/k") + val.Render(" or ") + lbl.Render("↑/↓") + "\n")
	sb.WriteString("  " + val.Render("Open detail with ") + lbl.Render("enter") + "\n")
	sb.WriteString("  " + val.Render("Filter with ") + lbl.Render("/") + "\n")
	sb.WriteString("  " + val.Render("Quick jump ") + lbl.Render("1-9") + val.Render(" · Back ") + lbl.Render("esc") + "\n")
	sb.WriteString("  " + val.Render("Command mode ") + lbl.Render(":") + val.Render(" · 8:Trader · 9:Trading · 0:Q AI") + "\n\n")

	connStatus := statusCritical.Render("● disconnected")
	if m.connected {
		connStatus = statusHealthy.Render("● connected")
	}
	sb.WriteString("  " + dim.Render("gateway: ") + val.Render(m.cfg.GatewayURL) + "\n")
	sb.WriteString("  " + dim.Render("status:  ") + connStatus + "\n\n")

	sb.WriteString("  " + dim.Render("YoorQuezt MEV Engine — Terminal Dashboard") + "\n")

	return sb.String()
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

func (m model) viewHeader() string {
	lbl := lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	val := lipgloss.NewStyle().Foreground(colorText)
	accent := lipgloss.NewStyle().Foreground(colorSecondary).Bold(true)

	// Connection indicator
	connIcon := statusCritical.Render("●")
	connLabel := statusCritical.Render("disconnected")
	if m.connected {
		connIcon = statusHealthy.Render("●")
		connLabel = statusHealthy.Render("connected")
	}

	// Left: logo + connection
	left := lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render("YQMEV") +
		lbl.Render(" · ") + connIcon + " " + connLabel

	if m.wsEvents > 0 {
		left += lbl.Render(" · ") + accent.Render(fmt.Sprintf("ws:%d", m.wsEvents))
	}

	// Right: gateway URL
	right := lbl.Render(m.cfg.GatewayURL)
	if !m.lastFetch.IsZero() {
		right += lbl.Render(" · ") + val.Render(m.lastFetch.Format("15:04:05"))
	}

	// Breadcrumb
	bc := m.viewBreadcrumb()

	headerW := m.width - 4
	if headerW < 40 {
		headerW = 40
	}

	// Layout
	pad := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if pad < 1 {
		pad = 1
	}
	topLine := left + strings.Repeat(" ", pad) + right
	bcLine := bc

	content := topLine + "\n" + bcLine

	return panelStyle.Width(headerW).Render(content)
}

func (m model) viewBreadcrumb() string {
	var parts []string
	for i, item := range m.breadcrumb {
		if i == len(m.breadcrumb)-1 {
			parts = append(parts, breadcrumbActive.Render(item))
		} else {
			parts = append(parts, breadcrumbItem.Render(item))
		}
	}
	return strings.Join(parts, breadcrumbSep.Render(" › "))
}

// ---------------------------------------------------------------------------
// Content routing
// ---------------------------------------------------------------------------

func (m model) viewContent() string {
	switch m.currentView {
	case viewDashboard:
		return m.viewDashboardContent()
	case viewBundles:
		return m.viewBundlesContent()
	case viewBlocks:
		return m.viewBlocksContent()
	case viewBlockDetail:
		return m.viewBlockDetailContent()
	case viewBundleDetail:
		return m.viewBundleDetailContent()
	case viewRelays:
		return m.viewRelaysContent()
	case viewIntents:
		return m.viewIntentsContent()
	case viewSystem:
		return m.viewSystemContent()
	case viewOFA:
		return m.viewOFAContent()
	case viewTrader:
		return m.viewTraderContent()
	case viewTrading:
		return m.viewTradingContent()
	case viewQAI:
		return m.viewQAIContent()
	case viewModelPicker:
		return m.viewModelPickerContent()
	}
	return ""
}

// ---------------------------------------------------------------------------
// Dashboard — card grid
// ---------------------------------------------------------------------------

func (m model) viewDashboardContent() string {
	cols := 3
	if m.width < 120 {
		cols = 2
	}
	if m.width < 80 {
		cols = 1
	}
	cardW := (m.width - (cols-1)*2 - 6) / cols
	if cardW < 20 {
		cardW = 20
	}

	type cardDef struct {
		title    string
		status   string
		lines    []string
		shortcut string
		label    string
	}

	cards := []cardDef{
		{
			title:    "Auction Pool",
			status:   statusFromCount(m.dashboard.poolSize),
			shortcut: "2",
			label:    "bundles",
			lines: []string{
				kv("Bundles", fmt.Sprintf("%d", m.dashboard.poolSize)),
				kvBar("Activity", m.dashboard.poolSize, 10, cardW-14),
				kv("Top Bid", orDash(m.dashboard.topBid)),
			},
		},
		{
			title:    "Block Builder",
			status:   statusFromCount(m.dashboard.blockCount),
			shortcut: "3",
			label:    "blocks",
			lines: []string{
				kv("Blocks Built", fmt.Sprintf("%d", m.dashboard.blockCount)),
				kv("Last Profit", orDash(m.dashboard.lastProfit) + " wei"),
				kvBar("Throughput", clamp(m.dashboard.blockCount, 0, 50)*2, 100, cardW-14),
			},
		},
		{
			title:    "Relay Marketplace",
			status:   "degraded",
			shortcut: "4",
			label:    "relays",
			lines: []string{
				kv("Relays", fmt.Sprintf("%d", m.relayCount)),
			},
		},
		{
			title:    "Intents & Solvers",
			status:   "degraded",
			shortcut: "5",
			label:    "intents",
			lines: []string{
				kv("Solvers", fmt.Sprintf("%d", m.solverCount)),
			},
		},
		{
			title:    "System Health",
			status:   boolToStatus(m.dashboard.healthy),
			shortcut: "6",
			label:    "system",
			lines: []string{
				kv("Engine", boolToLabel(m.dashboard.healthy)),
				kv("WS Events", fmt.Sprintf("%d", m.wsEvents)),
				kv("Connected", fmt.Sprintf("%v", m.connected)),
			},
		},
		{
			title:    "OFA Protection",
			status:   statusFromCount(m.ofa.txsProtected),
			shortcut: "7",
			label:    "ofa",
			lines: []string{
				kv("Protected", fmt.Sprintf("%d txs", m.ofa.txsProtected)),
				kv("Sandwiches Blocked", fmt.Sprintf("%d", m.ofa.sandwichBlocked)),
				kv("Pending Rebates", fmt.Sprintf("%d", m.ofa.pendingRebates)),
			},
		},
		{
			title:    "Trader",
			status:   statusFromCount(len(m.traderSubmissions)),
			shortcut: "8",
			label:    "trader",
			lines: []string{
				kv("Submissions", fmt.Sprintf("%d", len(m.traderSubmissions))),
			},
		},
		{
			title:    "Trading",
			status:   statusFromCount(len(m.tradingPositions)),
			shortcut: "9",
			label:    "trading",
			lines: []string{
				kv("Positions", fmt.Sprintf("%d", len(m.tradingPositions))),
				kv("Tokens", fmt.Sprintf("%d", len(m.tradingPortfolio))),
			},
		},
	}

	var cardRows []string
	for i := 0; i < len(cards); i += cols {
		var row []string
		for j := 0; j < cols && i+j < len(cards); j++ {
			idx := i + j
			c := cards[idx]
			selected := idx == m.dashCursor
			card := renderCard(c.title, c.status, c.lines, cardW, c.shortcut, selected)
			row = append(row, card)
		}
		cardRows = append(cardRows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
	}

	grid := lipgloss.JoinVertical(lipgloss.Left, cardRows...)

	// Summary health bar
	healthLine := renderHealthBar(m.dashboard, m.width-6)

	return healthLine + "\n\n" + grid + "\n\n" +
		mutedStyle.Render("  ↑↓/jk: select card  enter: open  2-9: jump  0:Q AI  :: command  r: refresh  q: quit")
}

func renderCard(title, status string, lines []string, width int, key string, selected bool) string {
	innerW := width - 6
	if innerW < 14 {
		innerW = 14
	}
	_ = innerW

	var icon string
	switch status {
	case "healthy":
		icon = statusHealthy.Render("●")
	case "degraded":
		icon = statusDegraded.Render("●")
	case "critical":
		icon = statusCritical.Render("●")
	default:
		icon = mutedStyle.Render("○")
	}

	cursor := "  "
	if selected {
		cursor = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render("▸ ")
	}

	keyLabel := mutedStyle.Render("press " + key)
	if selected {
		keyLabel = lipgloss.NewStyle().Foreground(colorSecondary).Bold(true).Render("press " + key + " or enter")
	}

	var sb strings.Builder
	sb.WriteString(cursor + lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(title) +
		"  " + icon + "\n")
	sb.WriteString("  " + keyLabel + "\n\n")
	for _, line := range lines {
		sb.WriteString("  " + line + "\n")
	}

	w := width - 4
	if selected {
		return panelSelectedStyle.Width(w).Render(sb.String())
	}

	switch status {
	case "healthy":
		return panelHealthyStyle.Width(w).Render(sb.String())
	case "degraded":
		return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(colorWarning).Padding(0, 1).Width(w).Render(sb.String())
	default:
		return panelStyle.Width(w).Render(sb.String())
	}
}

func renderHealthBar(d dashboardData, width int) string {
	parts := []string{
		statusHealthy.Render(fmt.Sprintf("%d bundles", d.poolSize)),
		statusHealthy.Render(fmt.Sprintf("%d blocks", d.blockCount)),
	}
	if d.healthy {
		parts = append(parts, statusHealthy.Render("engine ok"))
	} else {
		parts = append(parts, statusCritical.Render("engine down"))
	}
	content := "  " + strings.Join(parts, mutedStyle.Render(" · "))

	bar := panelStyle.Width(width).Render(content)
	return bar
}

// ---------------------------------------------------------------------------
// Bundles view — interactive list with sparklines
// ---------------------------------------------------------------------------

func (m model) viewBundlesContent() string {
	var sb strings.Builder
	st := m.bundleStats

	// ── Title + status ──
	titleParts := fmt.Sprintf("Auction Pool (%d bundles)", st.totalBundles)
	if m.bundlePaused {
		titleParts += "  " + statusDegraded.Render("⏸ PAUSED")
	} else {
		titleParts += "  " + statusHealthy.Render("● LIVE")
	}
	sb.WriteString(titleStyle.Render(titleParts))
	sb.WriteString("\n\n")

	if len(m.bundles) == 0 {
		sb.WriteString(mutedStyle.Render("  No bundles in pool. Waiting for arb detection...\n"))
		return sb.String()
	}

	// ── Aggregated stats panel ──
	statLine1 := fmt.Sprintf("  Avg Bid: %s  │  Max: %s  │  Min: %s  │  Pool Value: %s",
		weiToETH(fmt.Sprintf("%d", st.avgBidWei)),
		weiToETH(fmt.Sprintf("%d", st.maxBidWei)),
		weiToETH(fmt.Sprintf("%d", st.minBidWei)),
		weiToETH(fmt.Sprintf("%d", st.totalValueWei)),
	)
	sb.WriteString(lipgloss.NewStyle().Foreground(colorText).Render(statLine1))
	sb.WriteString("\n")

	simStyle := statusHealthy
	if st.simulatedPct < 50 {
		simStyle = statusDegraded
	}
	revStyle := statusHealthy
	if st.revertedPct > 20 {
		revStyle = statusCritical
	} else if st.revertedPct > 5 {
		revStyle = statusDegraded
	}
	statLine2 := fmt.Sprintf("  Simulated: %s  │  Reverted: %s  │  Blocks targeted: %d",
		simStyle.Render(fmt.Sprintf("%.0f%%", st.simulatedPct)),
		revStyle.Render(fmt.Sprintf("%.0f%%", st.revertedPct)),
		len(st.byBlock),
	)
	sb.WriteString(lipgloss.NewStyle().Foreground(colorText).Render(statLine2))
	sb.WriteString("\n")

	// ── Bid trend sparkline ──
	if len(st.bidHistory) > 1 {
		trendW := 30
		if m.width > 60 {
			trendW = 40
		}
		sb.WriteString(mutedStyle.Render("  Bid trend (60s): "))
		sb.WriteString(trendSparkline(st.bidHistory, trendW))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// ── Top 3 highest bids banner ──
	top3 := m.sortedBundles()
	if len(top3) > 3 {
		top3 = top3[:3]
	}
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorWarning).Render("  🏆 Top Bids"))
	sb.WriteString("\n")
	for i, b := range top3 {
		medal := []string{"🥇", "🥈", "🥉"}[i]
		sb.WriteString(fmt.Sprintf("  %s %s  %s  %s\n",
			medal,
			trunc(b.ID, 16),
			lipgloss.NewStyle().Bold(true).Foreground(colorSuccess).Render(weiToETH(b.EffectiveValue)),
			mutedStyle.Render(timeAgo(b.Timestamp)),
		))
	}
	sb.WriteString("\n")

	// ── Sort indicator + controls ──
	controls := fmt.Sprintf("  sort: %s (s)  │  %s (space)  │  filter: / ",
		lipgloss.NewStyle().Bold(true).Foreground(colorSecondary).Render(sortLabel(m.bundleSort)),
		func() string {
			if m.bundlePaused {
				return "resume"
			}
			return "pause"
		}(),
	)
	sb.WriteString(mutedStyle.Render(controls))
	sb.WriteString("\n\n")

	// ── Bundle table ──
	maxBid := int64(1)
	if st.maxBidWei > 0 {
		maxBid = st.maxBidWei
	}

	barW := 10
	hdr := fmt.Sprintf("  %-18s %-14s %-14s %-8s %-5s %-5s %s",
		"Bundle ID", "Bid", "Value", "Age", "Sim", "Rev", "Bid")
	sb.WriteString(headerStyle.Width(m.width).Render(hdr))
	sb.WriteString("\n")

	sorted := m.sortedBundles()
	for i, b := range sorted {
		prefix := "  "
		style := normalStyle
		if i == m.bundleCursor {
			prefix = "▸ "
			style = selectedStyle
		}

		// Color rows by status
		if b.Reverted == "true" {
			style = style.Foreground(colorDanger)
		} else if b.Simulated == "true" {
			style = style.Foreground(colorSuccess)
		}

		bidPct := int(parseBigInt(b.BidWei) * 100 / maxBid)
		simIcon := statusHealthy.Render("✓")
		if b.Simulated == "false" {
			simIcon = mutedStyle.Render("·")
		}
		revIcon := mutedStyle.Render("·")
		if b.Reverted == "true" {
			revIcon = statusCritical.Render("✗")
		}

		line := fmt.Sprintf("%s%-18s %-14s %-14s %-8s %-5s %-5s %s",
			prefix,
			trunc(b.ID, 18),
			weiToETH(b.BidWei),
			weiToETH(b.EffectiveValue),
			timeAgo(b.Timestamp),
			simIcon,
			revIcon,
			sparkBar(bidPct, barW),
		)
		sb.WriteString(style.Width(m.width).Render(line))
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m model) filteredBundles() []bundleEntry {
	if m.filterText == "" {
		return m.bundles
	}
	var out []bundleEntry
	for _, b := range m.bundles {
		if strings.Contains(strings.ToLower(b.ID), strings.ToLower(m.filterText)) ||
			strings.Contains(b.BidWei, m.filterText) {
			out = append(out, b)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Blocks view — interactive list
// ---------------------------------------------------------------------------

func (m model) viewBlocksContent() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Recent Blocks (%d)", len(m.blocks))))
	sb.WriteString("\n\n")

	if len(m.blocks) == 0 {
		sb.WriteString(mutedStyle.Render("  No blocks built yet.\n"))
		return sb.String()
	}

	// Normalize profit for sparkline
	maxProfit := int64(1)
	for _, b := range m.blocks {
		v := parseBigInt(b.TotalProfit)
		if v > maxProfit {
			maxProfit = v
		}
	}

	barW := 10
	hdr := fmt.Sprintf("  %-16s %-10s %-16s %-22s %s",
		"Block ID", "Bundles", "Profit (wei)", "Time", "Profit")
	sb.WriteString(headerStyle.Width(m.width).Render(hdr))
	sb.WriteString("\n")

	for i, b := range m.filteredBlocks() {
		prefix := "  "
		style := normalStyle
		if i == m.blockCursor {
			prefix = "▸ "
			style = selectedStyle
		}

		ts := formatTimestamp(b.Timestamp)
		profitPct := int(parseBigInt(b.TotalProfit) * 100 / maxProfit)

		bundleIcon := statusHealthy.Render(fmt.Sprintf("%d", b.BundleCount))
		if b.BundleCount >= 3 {
			bundleIcon = statusDegraded.Render(fmt.Sprintf("%d", b.BundleCount))
		}

		line := fmt.Sprintf("%s%-16s %-10s %-16s %-22s %s",
			prefix,
			trunc(b.ID, 16),
			bundleIcon,
			b.TotalProfit,
			ts,
			sparkBar(profitPct, barW),
		)
		sb.WriteString(style.Width(m.width).Render(line))
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m model) filteredBlocks() []blockEntry {
	if m.filterText == "" {
		return m.blocks
	}
	var out []blockEntry
	for _, b := range m.blocks {
		if strings.Contains(strings.ToLower(b.ID), strings.ToLower(m.filterText)) ||
			strings.Contains(b.TotalProfit, m.filterText) {
			out = append(out, b)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Block detail
// ---------------------------------------------------------------------------

func (m model) viewBlockDetailContent() string {
	if m.blockDetail == nil {
		return mutedStyle.Render("  Loading block detail...")
	}
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Block Detail"))
	sb.WriteString("\n\n")

	var pretty map[string]any
	if json.Unmarshal(m.blockDetail, &pretty) == nil {
		rendered, _ := json.MarshalIndent(pretty, "  ", "  ")
		sb.WriteString("  " + normalStyle.Render(string(rendered)))
	} else {
		sb.WriteString("  " + normalStyle.Render(string(m.blockDetail)))
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Bundle detail
// ---------------------------------------------------------------------------

func (m model) viewBundleDetailContent() string {
	sorted := m.sortedBundles()
	if m.bundleCursor >= len(sorted) {
		return mutedStyle.Render("  No bundle selected")
	}
	b := sorted[m.bundleCursor]

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Bundle Detail"))
	sb.WriteString("\n\n")

	// Status indicator
	statusLine := "  Status: "
	if b.Reverted == "true" {
		statusLine += statusCritical.Render("⊘ REVERTED")
	} else if b.Simulated == "true" {
		statusLine += statusHealthy.Render("✓ SIMULATED")
	} else {
		statusLine += statusDegraded.Render("◷ PENDING SIM")
	}
	sb.WriteString(statusLine + "\n\n")

	lines := []struct{ k, v string }{
		{"Bundle ID", b.ID},
		{"Bid", weiToETH(b.BidWei) + "  (" + b.BidWei + " wei)"},
		{"Effective Value", weiToETH(b.EffectiveValue) + "  (" + b.EffectiveValue + " wei)"},
		{"Chain", b.Chain},
		{"Target Block", b.TargetBlock},
		{"Age", timeAgo(b.Timestamp)},
		{"Timestamp", formatTimestamp(b.Timestamp)},
	}

	maxK := 0
	for _, l := range lines {
		if len(l.k) > maxK {
			maxK = len(l.k)
		}
	}

	for _, l := range lines {
		if l.v == "" || l.v == "  ( wei)" {
			continue
		}
		label := mutedStyle.Render(fmt.Sprintf("  %-*s  ", maxK, l.k))
		val := normalStyle.Render(l.v)
		sb.WriteString(label + val + "\n")
	}

	// Bid comparison vs pool
	if m.bundleStats.maxBidWei > 0 {
		bidVal := parseBigInt(b.BidWei)
		pctOfMax := float64(bidVal) / float64(m.bundleStats.maxBidWei) * 100
		pctOfAvg := float64(bidVal) / float64(m.bundleStats.avgBidWei) * 100

		sb.WriteString("\n")
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorSecondary).Render("  Pool Comparison"))
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("  vs max bid:  %s  %s\n",
			fmt.Sprintf("%.0f%%", pctOfMax),
			sparkBar(int(pctOfMax), 20),
		))
		sb.WriteString(fmt.Sprintf("  vs avg bid:  %s  %s\n",
			fmt.Sprintf("%.0f%%", pctOfAvg),
			sparkBar(clamp(int(pctOfAvg), 0, 100), 20),
		))
	}

	// Revert diagnostics for searchers
	if b.Reverted == "true" {
		sb.WriteString("\n")
		sb.WriteString(statusCritical.Render("  ⚠ Revert Analysis"))
		sb.WriteString("\n")
		sb.WriteString(mutedStyle.Render("  Possible causes:\n"))
		sb.WriteString(mutedStyle.Render("  • Stale state — pool reserves changed before inclusion\n"))
		sb.WriteString(mutedStyle.Render("  • Insufficient gas — bid didn't cover execution cost\n"))
		sb.WriteString(mutedStyle.Render("  • Frontrun — another searcher landed first\n"))
		sb.WriteString(mutedStyle.Render("  • Slippage — price moved beyond tolerance\n"))
		sb.WriteString(mutedStyle.Render("  Tip: Use `yqmev bundle simulate` to pre-check\n"))
	}

	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  esc: back  │  j/k: navigate  │  enter: select"))

	return sb.String()
}

// ---------------------------------------------------------------------------
// Relays view
// ---------------------------------------------------------------------------

func (m model) viewRelaysContent() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Relay Marketplace (%d)", m.relayCount)))
	sb.WriteString("\n\n")

	if len(m.relays) == 0 {
		sb.WriteString(mutedStyle.Render("  No relays registered.") + "\n")
		sb.WriteString(mutedStyle.Render("  (Relay marketplace runs as a separate service — start relayserver to see relays)") + "\n")
		return sb.String()
	}

	hdr := fmt.Sprintf("  %-18s %-16s %-30s %-8s %-8s",
		"Relay ID", "Name", "URL", "Score", "Active")
	sb.WriteString(headerStyle.Width(m.width).Render(hdr))
	sb.WriteString("\n")

	for i, r := range m.relays {
		prefix := "  "
		style := normalStyle
		if i == m.relayCursor {
			prefix = "▸ "
			style = selectedStyle
		}

		active := jstr(r, "active")
		activeIcon := statusHealthy.Render("●")
		if active == "false" {
			activeIcon = statusCritical.Render("●")
		}

		line := fmt.Sprintf("%s%-18s %-16s %-30s %-8s %s",
			prefix,
			trunc(jstr(r, "relay_id"), 18),
			jstr(r, "name"),
			trunc(jstr(r, "url"), 30),
			jstr(r, "score"),
			activeIcon,
		)
		sb.WriteString(style.Width(m.width).Render(line))
		sb.WriteString("\n")
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// Intents view
// ---------------------------------------------------------------------------

func (m model) viewIntentsContent() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Solvers & Intents (%d)", m.solverCount)))
	sb.WriteString("\n\n")

	if len(m.solvers) == 0 {
		sb.WriteString(mutedStyle.Render("  No solvers registered.") + "\n")
		sb.WriteString(mutedStyle.Render("  (Submit intents via CLI: yqctl intent submit)") + "\n")
		return sb.String()
	}

	hdr := fmt.Sprintf("  %-18s %-16s %-20s %-18s %-18s",
		"Solver ID", "Name", "Address", "Chains", "Intent Types")
	sb.WriteString(headerStyle.Width(m.width).Render(hdr))
	sb.WriteString("\n")

	for i, s := range m.solvers {
		prefix := "  "
		style := normalStyle
		if i == m.solverCursor {
			prefix = "▸ "
			style = selectedStyle
		}

		line := fmt.Sprintf("%s%-18s %-16s %-20s %-18s %-18s",
			prefix,
			trunc(jstr(s, "solver_id"), 18),
			jstr(s, "name"),
			trunc(jstr(s, "address"), 20),
			joinArr(s, "chains"),
			joinArr(s, "intent_types"),
		)
		sb.WriteString(style.Width(m.width).Render(line))
		sb.WriteString("\n")
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// System view — panels side by side
// ---------------------------------------------------------------------------

func (m model) viewSystemContent() string {
	var sb strings.Builder

	healthIcon := statusHealthy.Render("● HEALTHY")
	if !m.dashboard.healthy {
		healthIcon = statusCritical.Render("● UNHEALTHY")
	}
	sb.WriteString(titleStyle.Render("System Health") + "  " + healthIcon)
	sb.WriteString("\n\n")

	// Connection info panel
	connLines := []string{
		kv("Gateway", m.cfg.GatewayURL),
		kv("Connected", fmt.Sprintf("%v", m.connected)),
		kv("WS Events", fmt.Sprintf("%d", m.wsEvents)),
	}
	if !m.lastFetch.IsZero() {
		connLines = append(connLines, kv("Last Fetch", m.lastFetch.Format("15:04:05")))
	}

	panelW := m.width/3 - 4
	if panelW < 24 {
		panelW = m.width - 6
	}

	conn := panelStyle.Width(panelW).Render(
		titleStyle.Render("Connection") + "\n" + strings.Join(connLines, "\n"))

	ofLines := mapLines(m.systemOF, "")
	of := panelStyle.Width(panelW).Render(
		titleStyle.Render("Orderflow") + "\n" + strings.Join(ofLines, "\n"))

	cacheLines := mapLines(m.systemCache, "")
	cache := panelStyle.Width(panelW).Render(
		titleStyle.Render("Sim Cache") + "\n" + strings.Join(cacheLines, "\n"))

	if m.width >= 100 {
		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, conn, of, cache))
	} else {
		sb.WriteString(conn + "\n" + of + "\n" + cache)
	}

	sb.WriteString("\n\n")

	relayLines := mapLines(m.systemRelay, "")
	sb.WriteString(panelStyle.Width(m.width - 6).Render(
		titleStyle.Render("Relay Marketplace") + "\n" + strings.Join(relayLines, "\n")))

	return sb.String()
}

// ---------------------------------------------------------------------------
// Help bar — changes per view
// ---------------------------------------------------------------------------

func (m model) viewHelpBar() string {
	w := m.width
	if w < 40 {
		w = 40
	}
	style := helpBarStyle.Width(w)

	switch m.currentView {
	case viewDashboard:
		return style.Render("↑↓/jk: select  enter: open  2-9: jump  0:Q AI  :: command  r: refresh  q: quit")
	case viewBundles:
		return style.Render("↑↓/jk: navigate  enter: detail  esc: back  /: filter  :: command  r: refresh  q: quit")
	case viewBlocks:
		return style.Render("↑↓/jk: navigate  enter: detail  esc: back  /: filter  :: command  r: refresh  q: quit")
	case viewBlockDetail, viewBundleDetail:
		return style.Render("esc: back  :: command  q: quit")
	case viewRelays:
		return style.Render("↑↓/jk: navigate  esc: back  /: filter  :: command  r: refresh  q: quit")
	case viewIntents:
		return style.Render("↑↓/jk: navigate  esc: back  /: filter  :: command  r: refresh  q: quit")
	case viewSystem:
		return style.Render("r: refresh  esc: back  :: command  q: quit")
	case viewOFA:
		return style.Render("r: refresh  esc: back  :: command  q: quit")
	case viewTrader:
		return style.Render("tab: next field  enter: submit  esc: back  :: command  q: quit")
	case viewTrading:
		return style.Render("1: swap  2: positions  3: portfolio  tab: fields  enter: execute  esc: back  q: quit")
	case viewQAI:
		return style.Render("m: model picker  enter: send  esc: back  q: quit")
	case viewModelPicker:
		return style.Render("↑↓/jk: navigate  enter: select  esc: cancel  q: quit")
	default:
		return style.Render("q: quit")
	}
}

// ---------------------------------------------------------------------------
// OFA view — 3-panel layout
// ---------------------------------------------------------------------------

func (m model) viewOFAContent() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("OFA Protection Proxy"))
	sb.WriteString("\n\n")

	panelW := m.width/3 - 4
	if panelW < 24 {
		panelW = m.width - 6
	}

	// Stats panel
	statsLines := []string{
		kv("Txs Protected", fmt.Sprintf("%d", m.ofa.txsProtected)),
		kv("Sandwiches Blocked", fmt.Sprintf("%d", m.ofa.sandwichBlocked)),
		kv("MEV Captured", orDash(m.ofa.mevCaptured)),
	}
	statsPanel := panelStyle.Width(panelW).Render(
		titleStyle.Render("Stats") + "\n" + strings.Join(statsLines, "\n"))

	// Rebates panel
	rebateLines := []string{
		kv("Pending", fmt.Sprintf("%d", m.ofa.pendingRebates)),
		kv("Paid", fmt.Sprintf("%d", m.ofa.paidRebates)),
	}
	total := m.ofa.pendingRebates + m.ofa.paidRebates
	if total > 0 {
		paidPct := m.ofa.paidRebates * 100 / total
		rebateLines = append(rebateLines, kvBar("Completion", paidPct, 100, panelW-14))
	}
	rebatePanel := panelStyle.Width(panelW).Render(
		titleStyle.Render("Rebates") + "\n" + strings.Join(rebateLines, "\n"))

	// Recent events panel
	var eventLines []string
	if len(m.ofa.recentEvents) == 0 {
		eventLines = append(eventLines, mutedStyle.Render("No recent events"))
	}
	for _, evt := range m.ofa.recentEvents {
		evType := jstr(evt, "type")
		txID := trunc(jstr(evt, "tx_id"), 16)
		ts := jstr(evt, "timestamp")
		icon := statusHealthy.Render("●")
		if evType == "sandwich_detected" {
			icon = statusCritical.Render("●")
		}
		eventLines = append(eventLines, fmt.Sprintf("%s %s  %s  %s",
			icon,
			lipgloss.NewStyle().Foreground(colorSecondary).Render(evType),
			mutedStyle.Render(txID),
			mutedStyle.Render(ts),
		))
	}
	eventsPanel := panelStyle.Width(panelW).Render(
		titleStyle.Render("Recent Events") + "\n" + strings.Join(eventLines, "\n"))

	if m.width >= 100 {
		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, statsPanel, rebatePanel, eventsPanel))
	} else {
		sb.WriteString(statsPanel + "\n" + rebatePanel + "\n" + eventsPanel)
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// Trader view — submit form + recent submissions + auction watcher
// ---------------------------------------------------------------------------

func (m model) viewTraderContent() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Trader — Bundle Submission"))
	sb.WriteString("\n\n")

	leftW := m.width/2 - 4
	rightW := m.width/2 - 4
	if leftW < 30 {
		leftW = m.width - 6
		rightW = m.width - 6
	}

	// Left panel: submit form
	fieldLabels := []string{"Bid (wei)", "Chain", "TX JSON", "Target Block"}
	fieldInputs := []string{
		m.traderBidInput.View(),
		m.traderChainInput.View(),
		m.traderTxInput.View(),
		m.traderTargetInput.View(),
	}
	var formLines []string
	for i, lbl := range fieldLabels {
		cursor := "  "
		if i == m.traderFieldCursor {
			cursor = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render("▸ ")
		}
		formLines = append(formLines, cursor+mutedStyle.Render(lbl+":")+"\n    "+fieldInputs[i])
	}
	formLines = append(formLines, "")
	formLines = append(formLines, mutedStyle.Render("  tab: next field  enter: submit"))
	leftPanel := panelStyle.Width(leftW).Render(
		titleStyle.Render("Submit Bundle") + "\n" + strings.Join(formLines, "\n"))

	// Right panel: recent submissions
	var subLines []string
	if len(m.traderSubmissions) == 0 {
		subLines = append(subLines, mutedStyle.Render("No submissions yet."))
	}
	for i, s := range m.traderSubmissions {
		if i >= 15 {
			subLines = append(subLines, mutedStyle.Render(fmt.Sprintf("  ... and %d more", len(m.traderSubmissions)-15)))
			break
		}
		icon := mutedStyle.Render("○")
		if s.Status == "true" {
			icon = statusHealthy.Render("●")
		}
		subLines = append(subLines, fmt.Sprintf("%s %s  %s  %s",
			icon,
			mutedStyle.Render(trunc(s.BundleID, 16)),
			lipgloss.NewStyle().Foreground(colorSecondary).Render(s.Chain),
			normalStyle.Render(s.BidWei+" wei"),
		))
	}
	rightPanel := panelStyle.Width(rightW).Render(
		titleStyle.Render("Recent Submissions") + "\n" + strings.Join(subLines, "\n"))

	if m.width >= 80 {
		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel))
	} else {
		sb.WriteString(leftPanel + "\n" + rightPanel)
	}

	// Bottom: auction watcher
	sb.WriteString("\n\n")
	var auctionLines []string
	if len(m.traderAuctionEvts) == 0 {
		auctionLines = append(auctionLines, mutedStyle.Render("Waiting for auction events..."))
	}
	showCount := 8
	start := len(m.traderAuctionEvts) - showCount
	if start < 0 {
		start = 0
	}
	for _, evt := range m.traderAuctionEvts[start:] {
		// Attempt to parse for display
		var parsed map[string]any
		if json.Unmarshal([]byte(evt), &parsed) == nil {
			evType := jstr(parsed, "type")
			auctionLines = append(auctionLines,
				lipgloss.NewStyle().Foreground(colorSecondary).Render(evType)+"  "+
					mutedStyle.Render(trunc(evt, 80)))
		} else {
			auctionLines = append(auctionLines, mutedStyle.Render(trunc(evt, 80)))
		}
	}
	auctionPanel := panelStyle.Width(m.width - 6).Render(
		titleStyle.Render("Live Auction Watcher") + "\n" + strings.Join(auctionLines, "\n"))
	sb.WriteString(auctionPanel)

	return sb.String()
}

// ---------------------------------------------------------------------------
// Trader input handling
// ---------------------------------------------------------------------------

func (m *model) focusTraderField() {
	m.traderBidInput.Blur()
	m.traderChainInput.Blur()
	m.traderTxInput.Blur()
	m.traderTargetInput.Blur()
	switch m.traderFieldCursor {
	case 0:
		m.traderBidInput.Focus()
	case 1:
		m.traderChainInput.Focus()
	case 2:
		m.traderTxInput.Focus()
	case 3:
		m.traderTargetInput.Focus()
	}
}

func (m model) updateTraderInput(msg tea.KeyMsg) (model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.traderFieldCursor {
	case 0:
		m.traderBidInput, cmd = m.traderBidInput.Update(msg)
	case 1:
		m.traderChainInput, cmd = m.traderChainInput.Update(msg)
	case 2:
		m.traderTxInput, cmd = m.traderTxInput.Update(msg)
	case 3:
		m.traderTargetInput, cmd = m.traderTargetInput.Update(msg)
	}
	return m, cmd
}

func (m model) handleTraderSubmit() (model, tea.Cmd) {
	bidWei := m.traderBidInput.Value()
	chain := m.traderChainInput.Value()
	txJSON := m.traderTxInput.Value()
	targetBlock := m.traderTargetInput.Value()

	if bidWei == "" || txJSON == "" {
		m.lastCommandResult = "Bid and TX JSON are required"
		m.lastCommandError = true
		return m, nil
	}
	if chain == "" {
		chain = "ethereum"
	}

	c := m.client
	return m, func() tea.Msg {
		if c == nil {
			return traderSubmitResultMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Parse txs
		var txs []string
		if err := json.Unmarshal([]byte(txJSON), &txs); err != nil {
			// Treat as single tx
			txs = []string{txJSON}
		}

		params := map[string]any{
			"txs":          txs,
			"bid_wei":      bidWei,
			"chain":        chain,
			"target_block": targetBlock,
		}
		raw, err := c.Call(ctx, "mev_sendBundle", params)
		if err != nil {
			return traderSubmitResultMsg{err: err}
		}
		var resp map[string]any
		if json.Unmarshal(raw, &resp) == nil {
			return traderSubmitResultMsg{bundleID: jstr(resp, "bundle_id")}
		}
		return traderSubmitResultMsg{bundleID: string(raw)}
	}
}

// ---------------------------------------------------------------------------
// Trading view — swap execution, positions, portfolio
// ---------------------------------------------------------------------------

func (m model) viewTradingContent() string {
	var sb strings.Builder

	// Sub-tab header
	tabs := []struct {
		label string
		tab   tradingSubTab
	}{
		{"Swap", tradingSubSwap},
		{"Positions", tradingSubPositions},
		{"Portfolio", tradingSubPortfolio},
	}
	var tabParts []string
	for i, t := range tabs {
		key := fmt.Sprintf("%d", i+1)
		if t.tab == m.tradingSubTab {
			tabParts = append(tabParts,
				lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render(fmt.Sprintf("[%s] %s", key, t.label)))
		} else {
			tabParts = append(tabParts,
				mutedStyle.Render(fmt.Sprintf(" %s  %s", key, t.label)))
		}
	}
	sb.WriteString(titleStyle.Render("Trading") + "  " + strings.Join(tabParts, mutedStyle.Render("  ·  ")))
	sb.WriteString("\n\n")

	switch m.tradingSubTab {
	case tradingSubSwap:
		sb.WriteString(m.viewTradingSwap())
	case tradingSubPositions:
		sb.WriteString(m.viewTradingPositions())
	case tradingSubPortfolio:
		sb.WriteString(m.viewTradingPortfolio())
	}

	return sb.String()
}

func (m model) viewTradingSwap() string {
	var sb strings.Builder

	leftW := m.width/2 - 4
	rightW := m.width/2 - 4
	if leftW < 30 {
		leftW = m.width - 6
		rightW = m.width - 6
	}

	// Swap form
	fieldLabels := []string{"Token In", "Token Out", "Amount", "Chain", "Slippage %"}
	fieldInputs := []string{
		m.tradingTokenInInput.View(),
		m.tradingTokenOutInput.View(),
		m.tradingAmountInput.View(),
		m.tradingSwapChainInput.View(),
		m.tradingSlippageInput.View(),
	}
	var formLines []string
	for i, lbl := range fieldLabels {
		cursor := "  "
		if i == m.tradingSwapField {
			cursor = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render("▸ ")
		}
		formLines = append(formLines, cursor+mutedStyle.Render(lbl+":")+"\n    "+fieldInputs[i])
	}
	formLines = append(formLines, "")
	formLines = append(formLines, mutedStyle.Render("  tab: next field  enter: execute swap"))

	leftPanel := panelStyle.Width(leftW).Render(
		titleStyle.Render("Execute Swap") + "\n" + strings.Join(formLines, "\n"))

	// Right: swap info / last result
	var infoLines []string
	infoLines = append(infoLines, kv("Protected", "MEV protection via OFA"))
	infoLines = append(infoLines, kv("Routing", "Best price via DEX aggregation"))
	infoLines = append(infoLines, kv("Gas", "Auto-estimated"))
	infoLines = append(infoLines, "")
	if m.tradingLastSwap != nil {
		icon := statusHealthy.Render("●")
		if m.tradingLastSwap.Status != "confirmed" {
			icon = statusDegraded.Render("●")
		}
		infoLines = append(infoLines, icon+" "+kv("Last Swap", trunc(m.tradingLastSwap.TxHash, 24)))
		infoLines = append(infoLines, kv("Status", m.tradingLastSwap.Status))
	} else {
		infoLines = append(infoLines, mutedStyle.Render("No recent swaps"))
	}

	rightPanel := panelStyle.Width(rightW).Render(
		titleStyle.Render("Swap Info") + "\n" + strings.Join(infoLines, "\n"))

	if m.width >= 80 {
		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel))
	} else {
		sb.WriteString(leftPanel + "\n" + rightPanel)
	}

	return sb.String()
}

func (m model) viewTradingPositions() string {
	var sb strings.Builder

	if len(m.tradingPositions) == 0 {
		sb.WriteString(mutedStyle.Render("  No open positions.") + "\n")
		sb.WriteString(mutedStyle.Render("  Execute a swap to open a position.") + "\n")
		return sb.String()
	}

	hdr := fmt.Sprintf("  %-14s %-10s %-6s %-14s %-14s %-12s %-12s %-10s",
		"Pair", "Chain", "Side", "Entry", "Mark", "Size", "PnL", "PnL %")
	sb.WriteString(headerStyle.Width(m.width).Render(hdr))
	sb.WriteString("\n")

	for i, p := range m.tradingPositions {
		prefix := "  "
		style := normalStyle
		if i == m.tradingPosCursor {
			prefix = "▸ "
			style = selectedStyle
		}

		sideStyle := statusHealthy
		if p.Side == "short" {
			sideStyle = statusCritical
		}

		pnlStyle := statusHealthy
		if len(p.PnL) > 0 && p.PnL[0] == '-' {
			pnlStyle = statusCritical
		}

		line := fmt.Sprintf("%s%-14s %-10s %-6s %-14s %-14s %-12s %-12s %-10s",
			prefix,
			p.Pair,
			p.Chain,
			sideStyle.Render(p.Side),
			p.EntryPrice,
			p.MarkPrice,
			p.Size,
			pnlStyle.Render(p.PnL),
			pnlStyle.Render(p.PnLPct+"%"),
		)
		sb.WriteString(style.Width(m.width).Render(line))
		sb.WriteString("\n")
	}

	// Summary
	sb.WriteString("\n")
	totalPnL := 0.0
	for _, p := range m.tradingPositions {
		var v float64
		fmt.Sscanf(p.PnL, "%f", &v)
		totalPnL += v
	}
	pnlStyle := statusHealthy
	if totalPnL < 0 {
		pnlStyle = statusCritical
	}
	sb.WriteString("  " + kv("Total Positions", fmt.Sprintf("%d", len(m.tradingPositions))))
	sb.WriteString("    " + pnlStyle.Render(fmt.Sprintf("Total PnL: $%.2f", totalPnL)))
	sb.WriteString("\n")

	return sb.String()
}

func (m model) viewTradingPortfolio() string {
	var sb strings.Builder

	if len(m.tradingPortfolio) == 0 {
		sb.WriteString(mutedStyle.Render("  No tokens in portfolio.") + "\n")
		sb.WriteString(mutedStyle.Render("  Connect wallet or execute swaps to see holdings.") + "\n")
		return sb.String()
	}

	hdr := fmt.Sprintf("  %-10s %-12s %-18s %-14s %-10s",
		"Token", "Chain", "Balance", "Value (USD)", "24h")
	sb.WriteString(headerStyle.Width(m.width).Render(hdr))
	sb.WriteString("\n")

	totalUSD := 0.0
	for i, t := range m.tradingPortfolio {
		prefix := "  "
		style := normalStyle
		if i == m.tradingPortCursor {
			prefix = "▸ "
			style = selectedStyle
		}

		pctStyle := statusHealthy
		if len(t.PricePct) > 0 && t.PricePct[0] == '-' {
			pctStyle = statusCritical
		}

		var usd float64
		fmt.Sscanf(t.ValueUSD, "%f", &usd)
		totalUSD += usd

		line := fmt.Sprintf("%s%-10s %-12s %-18s $%-13s %s",
			prefix,
			t.Symbol,
			t.Chain,
			t.Balance,
			t.ValueUSD,
			pctStyle.Render(t.PricePct+"%"),
		)
		sb.WriteString(style.Width(m.width).Render(line))
		sb.WriteString("\n")
	}

	// Total value
	sb.WriteString("\n")
	sb.WriteString("  " + kv("Total Value", fmt.Sprintf("$%.2f", totalUSD)))
	sb.WriteString("    " + kv("Tokens", fmt.Sprintf("%d", len(m.tradingPortfolio))))
	sb.WriteString("\n")

	return sb.String()
}

// ---------------------------------------------------------------------------
// Trading input handling
// ---------------------------------------------------------------------------

func (m *model) focusTradingField() {
	m.tradingTokenInInput.Blur()
	m.tradingTokenOutInput.Blur()
	m.tradingAmountInput.Blur()
	m.tradingSwapChainInput.Blur()
	m.tradingSlippageInput.Blur()
	switch m.tradingSwapField {
	case 0:
		m.tradingTokenInInput.Focus()
	case 1:
		m.tradingTokenOutInput.Focus()
	case 2:
		m.tradingAmountInput.Focus()
	case 3:
		m.tradingSwapChainInput.Focus()
	case 4:
		m.tradingSlippageInput.Focus()
	}
}

func (m *model) blurTradingFields() {
	m.tradingTokenInInput.Blur()
	m.tradingTokenOutInput.Blur()
	m.tradingAmountInput.Blur()
	m.tradingSwapChainInput.Blur()
	m.tradingSlippageInput.Blur()
}

func (m model) updateTradingInput(msg tea.KeyMsg) (model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.tradingSwapField {
	case 0:
		m.tradingTokenInInput, cmd = m.tradingTokenInInput.Update(msg)
	case 1:
		m.tradingTokenOutInput, cmd = m.tradingTokenOutInput.Update(msg)
	case 2:
		m.tradingAmountInput, cmd = m.tradingAmountInput.Update(msg)
	case 3:
		m.tradingSwapChainInput, cmd = m.tradingSwapChainInput.Update(msg)
	case 4:
		m.tradingSlippageInput, cmd = m.tradingSlippageInput.Update(msg)
	}
	return m, cmd
}

func (m model) handleSwapSubmit() (model, tea.Cmd) {
	tokenIn := m.tradingTokenInInput.Value()
	tokenOut := m.tradingTokenOutInput.Value()
	amount := m.tradingAmountInput.Value()
	chain := m.tradingSwapChainInput.Value()
	slippage := m.tradingSlippageInput.Value()

	if tokenIn == "" || tokenOut == "" || amount == "" {
		m.lastCommandResult = "Token In, Token Out, and Amount are required"
		m.lastCommandError = true
		return m, nil
	}
	if chain == "" {
		chain = "ethereum"
	}
	if slippage == "" {
		slippage = "0.5"
	}

	c := m.client
	return m, func() tea.Msg {
		if c == nil {
			return swapResultMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		params := map[string]any{
			"token_in":  tokenIn,
			"token_out": tokenOut,
			"amount":    amount,
			"chain":     chain,
			"slippage":  slippage,
			"protected": true,
		}
		raw, err := c.Call(ctx, "trade_swap", params)
		if err != nil {
			return swapResultMsg{err: err}
		}
		var resp map[string]any
		if json.Unmarshal(raw, &resp) == nil {
			return swapResultMsg{result: swapResult{
				TxHash: jstr(resp, "tx_hash"),
				Status: jstr(resp, "status"),
			}}
		}
		return swapResultMsg{result: swapResult{TxHash: string(raw), Status: "submitted"}}
	}
}

// ---------------------------------------------------------------------------
// Command mode
// ---------------------------------------------------------------------------

func (m model) updateCommand(msg tea.KeyMsg) (model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		cmd := m.commandInput.Value()
		m.commandMode = false
		m.commandInput.Blur()
		return m, m.executeCommand(cmd)
	case "esc":
		m.commandMode = false
		m.commandInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.commandInput, cmd = m.commandInput.Update(msg)
	return m, cmd
}

func (m model) executeCommand(input string) tea.Cmd {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}
	c := m.client
	cmd := parts[0]
	args := parts[1:]

	return func() tea.Msg {
		if c == nil {
			return commandResultMsg{err: fmt.Errorf("not connected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		switch cmd {
		case "flush-rebates":
			raw, err := c.Call(ctx, "ofa_getPendingRebates", map[string]any{"flush": true})
			if err != nil {
				return commandResultMsg{err: fmt.Errorf("flush-rebates: %w", err)}
			}
			return commandResultMsg{result: "Rebates flushed: " + string(raw)}

		case "protect":
			if len(args) < 2 {
				return commandResultMsg{err: fmt.Errorf("usage: protect <chain> <raw_tx>")}
			}
			chain := args[0]
			rawTx := args[1]
			raw, err := c.Call(ctx, "mev_protectTx", map[string]any{
				"chain":   chain,
				"payload": rawTx,
			})
			if err != nil {
				return commandResultMsg{err: fmt.Errorf("protect: %w", err)}
			}
			return commandResultMsg{result: "Protected: " + string(raw)}

		case "rpc":
			if len(args) < 2 {
				return commandResultMsg{err: fmt.Errorf("usage: rpc <chain> <method> [params]")}
			}
			method := args[1]
			var params any
			if len(args) > 2 {
				paramStr := strings.Join(args[2:], " ")
				if err := json.Unmarshal([]byte(paramStr), &params); err != nil {
					params = paramStr
				}
			}
			raw, err := c.Call(ctx, method, params)
			if err != nil {
				return commandResultMsg{err: fmt.Errorf("rpc %s: %w", method, err)}
			}
			return commandResultMsg{result: string(raw)}

		case "register":
			if len(args) < 1 {
				return commandResultMsg{err: fmt.Errorf("usage: register <name>")}
			}
			raw, err := c.Call(ctx, "mev_relayRegister", map[string]any{"name": args[0]})
			if err != nil {
				return commandResultMsg{err: fmt.Errorf("register: %w", err)}
			}
			return commandResultMsg{result: "Registered: " + string(raw)}

		case "stats":
			raw, err := c.Call(ctx, "yq_getStats", nil)
			if err != nil {
				return commandResultMsg{err: fmt.Errorf("stats: %w", err)}
			}
			var pretty map[string]any
			if json.Unmarshal(raw, &pretty) == nil {
				out, _ := json.MarshalIndent(pretty, "", "  ")
				return commandResultMsg{result: string(out)}
			}
			return commandResultMsg{result: string(raw)}

		case "health":
			raw, err := c.Health(ctx)
			if err != nil {
				return commandResultMsg{err: fmt.Errorf("health: %w", err)}
			}
			return commandResultMsg{result: string(raw)}

		default:
			return commandResultMsg{err: fmt.Errorf("unknown command: %s (available: flush-rebates, protect, rpc, register, stats, health)", cmd)}
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func jstr(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return "-"
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%.4f", val)
	case nil:
		return "-"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func jfloat(m map[string]interface{}, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	f, _ := v.(float64)
	return f
}

func jint(m map[string]interface{}, key string) int64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	f, _ := v.(float64)
	return int64(f)
}

func joinArr(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return "-"
	}
	arr, ok := v.([]interface{})
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	parts := make([]string, len(arr))
	for i, item := range arr {
		parts[i] = fmt.Sprintf("%v", item)
	}
	return strings.Join(parts, ",")
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 4 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func kv(label, value string) string {
	return mutedStyle.Render(label+": ") + normalStyle.Bold(true).Render(value)
}

func kvBar(label string, val, max, barWidth int) string {
	pct := 0
	if max > 0 {
		pct = val * 100 / max
	}
	return mutedStyle.Render(label+": ") + sparkBar(pct, barWidth)
}

func mapLines(m map[string]any, prefix string) []string {
	if m == nil {
		return []string{mutedStyle.Render(prefix + "(not available)")}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var lines []string
	for _, k := range keys {
		lines = append(lines, kv(prefix+k, fmt.Sprintf("%v", m[k])))
	}
	return lines
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func orDash(s string) string {
	if s == "" || s == "-" {
		return "-"
	}
	return s
}

func parseBigInt(s string) int64 {
	if s == "" || s == "-" {
		return 0
	}
	var v int64
	fmt.Sscanf(s, "%d", &v)
	return v
}

func formatTimestamp(ts int64) string {
	if ts == 0 {
		return "-"
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05")
}

func weiToETH(weiStr string) string {
	v := parseBigInt(weiStr)
	if v == 0 {
		return "0 ETH"
	}
	eth := float64(v) / 1e18
	if eth >= 1.0 {
		return fmt.Sprintf("%.4f ETH", eth)
	}
	if eth >= 0.001 {
		return fmt.Sprintf("%.6f ETH", eth)
	}
	// Show in gwei for tiny amounts
	gwei := float64(v) / 1e9
	if gwei >= 1.0 {
		return fmt.Sprintf("%.2f gwei", gwei)
	}
	return fmt.Sprintf("%d wei", v)
}

func timeAgo(ts int64) string {
	if ts == 0 {
		return "-"
	}
	d := time.Since(time.Unix(ts, 0))
	switch {
	case d < time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

func computeBundleStats(bundles []bundleEntry, prevHistory []int64) bundleStats {
	s := bundleStats{
		totalBundles: len(bundles),
		byBlock:      make(map[string]int),
		minBidWei:    1<<63 - 1,
	}
	if len(bundles) == 0 {
		s.minBidWei = 0
		return s
	}

	var simCount, revCount int
	var totalBid int64
	for _, b := range bundles {
		v := parseBigInt(b.BidWei)
		totalBid += v
		if v > s.maxBidWei {
			s.maxBidWei = v
		}
		if v < s.minBidWei {
			s.minBidWei = v
		}
		s.totalValueWei += parseBigInt(b.EffectiveValue)
		if b.Simulated == "true" {
			simCount++
		}
		if b.Reverted == "true" {
			revCount++
		}
		if b.TargetBlock != "" {
			s.byBlock[b.TargetBlock]++
		}
	}
	s.avgBidWei = totalBid / int64(len(bundles))
	s.simulatedPct = float64(simCount) / float64(len(bundles)) * 100
	s.revertedPct = float64(revCount) / float64(len(bundles)) * 100

	// Append current max bid to history, keep last 60 entries
	s.bidHistory = append(prevHistory, s.maxBidWei)
	if len(s.bidHistory) > 60 {
		s.bidHistory = s.bidHistory[len(s.bidHistory)-60:]
	}
	return s
}

// trendSparkline renders a mini sparkline chart from a series of values.
func trendSparkline(values []int64, width int) string {
	if len(values) == 0 {
		return mutedStyle.Render(strings.Repeat("·", width))
	}
	sparks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	// Find range
	minV, maxV := values[0], values[0]
	for _, v := range values {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}

	span := maxV - minV
	if span == 0 {
		span = 1
	}

	// Sample values to fit width
	var result strings.Builder
	step := len(values) / width
	if step < 1 {
		step = 1
	}
	for i := 0; i < width && i*step < len(values); i++ {
		idx := i * step
		level := int((values[idx] - minV) * 7 / span)
		if level < 0 {
			level = 0
		}
		if level > 7 {
			level = 7
		}
		result.WriteRune(sparks[level])
	}
	// Pad if needed
	for result.Len() < width {
		result.WriteRune(sparks[0])
	}
	return lipgloss.NewStyle().Foreground(colorSecondary).Render(result.String())
}

func (m model) sortedBundles() []bundleEntry {
	filtered := m.filteredBundles()
	sorted := make([]bundleEntry, len(filtered))
	copy(sorted, filtered)

	switch m.bundleSort {
	case 0: // value desc
		sort.Slice(sorted, func(i, j int) bool {
			return parseBigInt(sorted[i].EffectiveValue) > parseBigInt(sorted[j].EffectiveValue)
		})
	case 1: // time desc (newest first)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Timestamp > sorted[j].Timestamp
		})
	case 2: // id (default)
		// keep original order
	}
	return sorted
}

func sortLabel(idx int) string {
	switch idx {
	case 0:
		return "value"
	case 1:
		return "time"
	default:
		return "default"
	}
}

func statusFromCount(n int) string {
	if n > 0 {
		return "healthy"
	}
	return "degraded"
}

func boolToStatus(v bool) string {
	if v {
		return "healthy"
	}
	return "critical"
}

func boolToLabel(v bool) string {
	if v {
		return statusHealthy.Render("● running")
	}
	return statusCritical.Render("● down")
}

// ---------------------------------------------------------------------------
// Q AI — methods
// ---------------------------------------------------------------------------

func (m model) handleAISend() (model, tea.Cmd) {
	q := strings.TrimSpace(m.aiInput.Value())
	if q == "" || m.aiStreaming || m.aiProvider == nil {
		return m, nil
	}

	m.aiMessages = append(m.aiMessages, ai.Message{Role: "user", Content: q})
	m.aiInput.SetValue("")
	m.aiStreaming = true
	m.aiStreamBuf = ""
	m.aiQState = 2 // thinking

	provider := m.aiProvider
	history := make([]ai.Message, len(m.aiMessages)-1)
	copy(history, m.aiMessages[:len(m.aiMessages)-1])

	mevCtx := ai.MEVContext{
		GatewayURL:       m.cfg.GatewayURL,
		Connected:        m.connected,
		Healthy:          m.dashboard.healthy,
		PoolSize:         m.dashboard.poolSize,
		BlockCount:       m.dashboard.blockCount,
		WSEvents:         m.wsEvents,
		BundleCount:      len(m.bundles),
		TopBid:           m.dashboard.topBid,
		LastProfit:        m.dashboard.lastProfit,
		ActiveRelays:     m.dashboard.relayCount,
		RegisteredRelays: m.dashboard.relayCount,
		TxsProtected:     m.ofa.txsProtected,
		SandwichBlocked:  m.ofa.sandwichBlocked,
		MEVCaptured:      m.ofa.mevCaptured,
		SolverCount:      m.solverCount,
	}

	tokenCh := make(chan string, 64)
	errCh := make(chan error, 1)

	go func() {
		errCh <- provider.Stream(context.Background(), ai.AnalyzeRequest{
			Question:   q,
			MEVContext: mevCtx,
			History:    history,
		}, tokenCh)
	}()

	m.aiTokenCh = tokenCh
	m.aiErrCh = errCh

	return m, waitForToken(tokenCh, errCh)
}

func waitForToken(tokenCh <-chan string, errCh <-chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case tok, ok := <-tokenCh:
			if !ok {
				// Channel closed, get the error
				err := <-errCh
				return aiDoneMsg{err: err}
			}
			return aiTokenMsg{token: tok}
		case err := <-errCh:
			// Error before channel closed — drain remaining
			for range tokenCh {
			}
			return aiDoneMsg{err: err}
		}
	}
}

func (m model) buildMEVContext() ai.MEVContext {
	return ai.MEVContext{
		GatewayURL:       m.cfg.GatewayURL,
		Connected:        m.connected,
		Healthy:          m.dashboard.healthy,
		PoolSize:         m.dashboard.poolSize,
		BlockCount:       m.dashboard.blockCount,
		WSEvents:         m.wsEvents,
		BundleCount:      len(m.bundles),
		TopBid:           m.dashboard.topBid,
		LastProfit:        m.dashboard.lastProfit,
		ActiveRelays:     m.dashboard.relayCount,
		RegisteredRelays: m.dashboard.relayCount,
		TxsProtected:     m.ofa.txsProtected,
		SandwichBlocked:  m.ofa.sandwichBlocked,
		MEVCaptured:      m.ofa.mevCaptured,
		SolverCount:      m.solverCount,
		MCPAvailable:     m.mcpAvailable,
	}
}

func (m model) viewQAIContent() string {
	var sb strings.Builder

	// Q mascot compact
	qFace := renderQCompactMEV(m.aiQState, m.frame)
	aiName := ""
	if m.aiProvider != nil {
		aiName = m.aiProvider.Name()
	}

	sb.WriteString(headerStyle.Render("Q AI MEV Assistant") + "  " + qFace)
	if aiName != "" {
		sb.WriteString("  " + mutedStyle.Render("("+aiName+")"))
	}
	sb.WriteString("\n\n")

	if m.aiProvider == nil {
		sb.WriteString(statusCritical.Render("  No AI provider configured.") + "\n\n")
		sb.WriteString(normalStyle.Render("  Set one of:") + "\n")
		sb.WriteString(statusHealthy.Render("    ANTHROPIC_API_KEY") + normalStyle.Render("  — Claude (recommended)") + "\n")
		sb.WriteString(statusHealthy.Render("    OPENAI_API_KEY") + normalStyle.Render("    — OpenAI GPT-4o") + "\n")
		sb.WriteString(statusHealthy.Render("    OLLAMA_MODEL") + normalStyle.Render("      — Ollama (free, local)") + "\n\n")
		sb.WriteString(mutedStyle.Render("  Example: ANTHROPIC_API_KEY=sk-ant-... yqtui") + "\n")
		return sb.String()
	}

	// Chat history
	for _, msg := range m.aiMessages {
		if msg.Role == "user" {
			sb.WriteString(lipgloss.NewStyle().Foreground(colorSecondary).Bold(true).Render("  You: "))
			sb.WriteString(normalStyle.Render(msg.Content) + "\n\n")
		} else {
			sb.WriteString(lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("  Q: "))
			// Render response with code block detection
			sb.WriteString(renderAIResponse(msg.Content) + "\n\n")
		}
	}

	// Streaming indicator
	if m.aiStreaming {
		dots := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		if m.aiStreamBuf != "" {
			sb.WriteString(lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("  Q: "))
			sb.WriteString(renderAIResponse(m.aiStreamBuf))
			sb.WriteString(lipgloss.NewStyle().Foreground(colorSuccess).Render(dots[m.frame%len(dots)]) + "\n\n")
		} else {
			sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSuccess).Render("Q "+dots[m.frame%len(dots)]+" thinking...") + "\n\n")
		}
	}

	// Empty state
	if len(m.aiMessages) == 0 && !m.aiStreaming {
		sb.WriteString(renderQMascotMEV(m.frame))
		sb.WriteString("\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("  Hi! I'm Q.") + "\n")
		sb.WriteString(mutedStyle.Render("  Ask me about MEV activity, bundles, relays, or anything.") + "\n\n")
		sb.WriteString(mutedStyle.Render("  Try:") + "\n")
		sb.WriteString(normalStyle.Render("  • What sandwiches were blocked recently?") + "\n")
		sb.WriteString(normalStyle.Render("  • Analyze the top bundle bid") + "\n")
		sb.WriteString(normalStyle.Render("  • How many relays are active?") + "\n")
		sb.WriteString(normalStyle.Render("  • Explain MEV extraction strategies") + "\n\n")
		sb.WriteString(mutedStyle.Render("  Press m to change AI model") + "\n\n")
	}

	// Input
	sb.WriteString("  " + m.aiInput.View() + "\n")

	return sb.String()
}

func ratingBar(level int) string {
	filled := "█"
	empty := "░"
	var sb strings.Builder
	for i := 0; i < 5; i++ {
		if i < level {
			sb.WriteString(filled)
		} else {
			sb.WriteString(empty)
		}
	}
	return sb.String()
}

func providerTag(provider string) string {
	switch provider {
	case "claude":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#CC785C")).Bold(true).Render("Claude")
	case "openai":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#74AA9C")).Bold(true).Render("OpenAI")
	case "ollama":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#1E90FF")).Bold(true).Render("Ollama")
	}
	return provider
}

func (m model) viewModelPickerContent() string {
	var sb strings.Builder

	sb.WriteString(headerStyle.Render("Select AI Model"))
	sb.WriteString("\n\n")

	currentName := ""
	if m.aiProvider != nil {
		currentName = m.aiProvider.Name()
	}

	// Column headers
	sb.WriteString(mutedStyle.Render(fmt.Sprintf("  %-22s %-8s %-14s %-14s %-14s", "MODEL", "PROVIDER", "INTELLIGENCE", "SPEED", "COST")))
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  " + strings.Repeat("─", 76)))
	sb.WriteString("\n")

	for i, mo := range availableModels {
		isCurrent := mo.Name == currentName || mo.Model == currentName
		isSelected := i == m.modelPickerCursor

		name := fmt.Sprintf("%-20s", mo.Name)
		tag := providerTag(mo.Provider)

		intBar := ratingBar(mo.Intelligence)
		spdBar := ratingBar(mo.Speed)

		var costStr string
		if mo.Cost == 0 {
			costStr = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("FREE ") + "░░░░░"
		} else {
			costStr = ratingBar(mo.Cost) + "     "
		}

		currentMarker := "  "
		if isCurrent {
			currentMarker = lipgloss.NewStyle().Foreground(colorSuccess).Render("● ")
		}

		line := fmt.Sprintf("%s%-20s  %-14s  %s  %s  %s",
			currentMarker, name, tag, intBar, spdBar, costStr)

		if isSelected {
			sb.WriteString(selectedStyle.Width(m.width - 4).Render(line))
		} else {
			sb.WriteString(normalStyle.Render(line))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  ● = current model"))
	sb.WriteString("\n")

	if m.err != nil {
		sb.WriteString("\n")
		sb.WriteString(statusCritical.Render("  Error: " + m.err.Error()))
		sb.WriteString("\n")
	}

	return sb.String()
}

func renderAIResponse(content string) string {
	lines := strings.Split(content, "\n")
	var sb strings.Builder
	inCode := false

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			if inCode {
				sb.WriteString("\n" + lipgloss.NewStyle().Foreground(colorMuted).Render("  ┌─────────────────────────────────────") + "\n")
			} else {
				sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("  └─────────────────────────────────────") + "\n")
			}
			continue
		}
		if inCode {
			sb.WriteString(lipgloss.NewStyle().Foreground(colorSuccess).Render("  │ "+line) + "\n")
		} else {
			sb.WriteString(normalStyle.Render(line) + "\n")
		}
	}
	return sb.String()
}

func renderQCompactMEV(state, frame int) string {
	// Q face states matching q8s
	G := lipgloss.NewStyle().Background(lipgloss.Color("#5CDB95"))
	E := lipgloss.NewStyle().Background(lipgloss.Color("#1E3A3A")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
	M := lipgloss.NewStyle().Background(lipgloss.Color("#1E3A3A")).Foreground(lipgloss.Color("#A8E6CF"))
	X := lipgloss.NewStyle().Background(lipgloss.Color("#1E3A3A")).Foreground(lipgloss.Color("#EF4444"))
	K := lipgloss.NewStyle().Background(lipgloss.Color("#1E3A3A")).Foreground(lipgloss.Color("#22C55E"))
	Th := lipgloss.NewStyle().Foreground(lipgloss.Color("#06B6D4"))
	Sp := lipgloss.NewStyle().Foreground(lipgloss.Color("#5CDB95"))

	switch state {
	case 1: // listening
		return G.Render(" ") + E.Render("◉") + M.Render("○") + E.Render("◉") + G.Render(" ")
	case 2: // thinking
		return G.Render(" ") + E.Render("◑") + M.Render("─") + E.Render("◑") + G.Render(" ") + Th.Render("···")
	case 3: // talking
		return G.Render(" ") + E.Render("◕") + M.Render("○") + E.Render("◕") + G.Render(" ") + Sp.Render("♪")
	case 4: // success
		return G.Render(" ") + E.Render("◕") + K.Render("◡") + E.Render("◕") + G.Render(" ") + K.Render("✓")
	case 5: // error
		return G.Render(" ") + X.Render("×") + X.Render("︵") + X.Render("×") + G.Render(" ")
	default: // idle
		return G.Render(" ") + E.Render("◕") + M.Render("‿") + E.Render("◕") + G.Render(" ")
	}
}

func renderQMascotMEV(frame int) string {
	G := lipgloss.NewStyle().Background(lipgloss.Color("#5CDB95"))
	D := lipgloss.NewStyle().Background(lipgloss.Color("#1E3A3A"))
	E := lipgloss.NewStyle().Background(lipgloss.Color("#1E3A3A")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
	M := lipgloss.NewStyle().Background(lipgloss.Color("#1E3A3A")).Foreground(lipgloss.Color("#A8E6CF"))
	Sp := lipgloss.NewStyle().Foreground(lipgloss.Color("#5CDB95"))

	stars := []string{"✦", "·", "✧", "·"}
	s1 := stars[frame%4]
	s2 := stars[(frame+2)%4]

	var sb strings.Builder
	sb.WriteString("      " + Sp.Render(s1) + "                " + Sp.Render(s2) + "\n")
	sb.WriteString("        " + G.Render("              ") + "\n")
	sb.WriteString("       " + G.Render("                ") + "\n")
	sb.WriteString("       " + G.Render("  ") + D.Render("            ") + G.Render("  ") + "\n")
	sb.WriteString("       " + G.Render("  ") + E.Render("  ◕") + D.Render("      ") + E.Render("◕  ") + G.Render("  ") + "\n")
	sb.WriteString("       " + G.Render("  ") + D.Render("   ") + M.Render("◡◡◡◡◡◡") + D.Render("   ") + G.Render("  ") + "\n")
	sb.WriteString("       " + G.Render("  ") + D.Render("            ") + G.Render("  ") + "\n")
	sb.WriteString("       " + G.Render("                ") + "\n")
	sb.WriteString("        " + G.Render("              ") + "\n")
	sb.WriteString("             " + G.Render("    ") + "\n")
	sb.WriteString("              " + G.Render("  ") + "\n")
	return sb.String()
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	gwURL := flag.String("gw", envOr("YQMEV_GATEWAY", "ws://localhost:9099/ws"), "Gateway WebSocket URL")
	apiKey := flag.String("key", envOr("YQMEV_API_KEY", ""), "API key")
	aiProviderFlag := flag.String("ai", envOr("YQMEV_AI_PROVIDER", "claude"), "AI provider: claude, openai, ollama")
	aiModelFlag := flag.String("ai-model", envOr("YQMEV_AI_MODEL", ""), "AI model override")
	mcpURL := flag.String("mcp", envOr("YQMEV_MCP_URL", "http://localhost:3101"), "MCP MEV server URL")
	logDir := flag.String("log-dir", envOr("YQMEV_LOG_DIR", ""), "Log directory (default: ~/.yqmev/logs/)")
	flag.Parse()

	// Initialize zap logger
	if err := logging.Init(*logDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: logging init failed: %v\n", err)
	}
	defer logging.Close()
	logging.Info("yqmev starting", "gateway", *gwURL, "ai", *aiProviderFlag, "mcp", *mcpURL)

	cfg := client.Config{
		GatewayURL: *gwURL,
		APIKey:     *apiKey,
	}

	m := initialModel(cfg)

	// Initialize AI provider
	switch *aiProviderFlag {
	case "claude":
		key := envOr("ANTHROPIC_API_KEY", envOr("YQMEV_AI_KEY", ""))
		if key != "" {
			m.aiProvider = ai.NewClaude(key, *aiModelFlag)
		}
	case "openai":
		key := envOr("OPENAI_API_KEY", envOr("YQMEV_AI_KEY", ""))
		if key != "" {
			m.aiProvider = ai.NewOpenAI(key, *aiModelFlag, "")
		}
	case "ollama":
		model := *aiModelFlag
		if model == "" {
			model = envOr("OLLAMA_MODEL", "llama3.1")
		}
		m.aiProvider = ai.NewOllama(model, envOr("OLLAMA_URL", ""))
	}

	// Initialize MCP client
	mc := mcp.NewClient(*mcpURL)
	m.mcpClient = mc
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	m.mcpAvailable = mc.Healthy(ctx)
	cancel()
	logging.Info("mcp status", "url", *mcpURL, "available", m.mcpAvailable)

	if m.aiProvider != nil {
		logging.Info("ai provider ready", "provider", m.aiProvider.Name())
	} else {
		logging.Warn("no ai provider configured")
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	logging.Info("starting TUI")
	if _, err := p.Run(); err != nil {
		logging.Error("TUI exited with error", "error", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	logging.Info("yqmev shutdown")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
