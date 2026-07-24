package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	apiclient "github.com/compozy/compozy/internal/api/client"
	"github.com/compozy/compozy/internal/api/contract"
	apicore "github.com/compozy/compozy/internal/api/core"
	"github.com/compozy/compozy/pkg/compozy/events"

	tea "charm.land/bubbletea/v2"
)

const (
	convergenceScrollPage    = 5
	convergenceActionTimeout = 30 * time.Second
)

var (
	setupRemoteConvergenceUISession = newConvergenceController
	newConvergenceTeaProgram        = defaultNewConvergenceTeaProgram
)

// RemoteConvergenceAttachOptions configures a daemon-backed convergence UI attach
// session. The convergence snapshot is the authoritative projected state; the run
// event stream only signals when to reload it, so the UI never reconstructs truth
// from event prose or displayed counts.
type RemoteConvergenceAttachOptions struct {
	Snapshot        apicore.RunSnapshot
	Convergence     contract.ConvergenceSnapshot
	HasConvergence  bool
	Config          *config
	WorkspaceRoot   string
	OwnerSession    bool
	LoadRunSnapshot func(context.Context) (apicore.RunSnapshot, error)
	LoadConvergence func(context.Context) (contract.ConvergenceSnapshot, error)
	OpenStream      func(context.Context, apicore.StreamCursor) (apiclient.RunStream, error)
	Approve         func(context.Context, contract.ApprovalDecisionRequest) error
	Resume          func(context.Context, contract.ConvergenceResumeRequest) error
	Cancel          func(context.Context) error
}

type convergenceSnapshotMsg struct {
	snapshot contract.ConvergenceSnapshot
}

type convergenceActionResultMsg struct {
	action convergenceAction
	err    error
}

type convergenceModel struct {
	parentRun        apicore.Run
	snapshot         contract.ConvergenceSnapshot
	view             convergenceView
	hasSnapshot      bool
	width            int
	height           int
	scroll           int
	cfg              *config
	quitDialog       quitDialogState
	prompt           convergencePrompt
	lastError        string
	onQuit           func(uiQuitRequest)
	now              time.Time
	loadConvergence  func(context.Context) (contract.ConvergenceSnapshot, error)
	approve          func(context.Context, contract.ApprovalDecisionRequest) error
	resume           func(context.Context, contract.ConvergenceResumeRequest) error
	cancel           func(context.Context) error
	newActionContext func() (context.Context, context.CancelFunc)
}

type convergenceController struct {
	model         *convergenceModel
	prog          *tea.Program
	done          chan error
	quitHandler   func(uiQuitRequest)
	quitHandlerMu sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	workers       sync.WaitGroup
	shutdownOnce  sync.Once
}

// AttachRemoteConvergence boots the dedicated convergence cockpit from a
// daemon-owned run. It reloads the bounded convergence snapshot on every stream
// event and follows the run stream with the shared replay-then-live engine.
func AttachRemoteConvergence(ctx context.Context, opts RemoteConvergenceAttachOptions) (Session, error) {
	mdl := newRemoteConvergenceModel(opts)
	session := setupRemoteConvergenceUISession(ctx, mdl)
	if session == nil {
		return nil, errors.New("remote convergence ui session is required")
	}
	reloadCh := make(chan struct{}, 1)
	requestReload := func() {
		select {
		case reloadCh <- struct{}{}:
		default:
		}
	}
	if opts.LoadConvergence != nil {
		startRemoteWorker(session, func(workerCtx context.Context) {
			reloadConvergenceSnapshot(workerCtx, session, opts.LoadConvergence)
			for {
				select {
				case <-workerCtx.Done():
					return
				case <-reloadCh:
					reloadConvergenceSnapshot(workerCtx, session, opts.LoadConvergence)
				}
			}
		})
	}
	if opts.OpenStream != nil {
		stream, err := opts.OpenStream(ctx, apicore.StreamCursor{})
		if err != nil {
			session.Shutdown()
			return nil, fmt.Errorf("open remote convergence stream: %w", err)
		}
		streamSession := convergenceStreamSession{Session: session, onEvent: func(events.Event) { requestReload() }}
		startRemoteWorker(session, func(workerCtx context.Context) {
			followRemoteConvergence(workerCtx, streamSession, opts, stream)
		})
	}
	return session, nil
}

func newRemoteConvergenceModel(opts RemoteConvergenceAttachOptions) *convergenceModel {
	cfg := opts.Config
	if cfg == nil {
		cfg = &config{}
	}
	localCfg := *cfg
	localCfg.DetachOnly = !opts.OwnerSession
	localCfg.DaemonOwned = true
	if workspaceRoot := strings.TrimSpace(opts.WorkspaceRoot); workspaceRoot != "" {
		localCfg.WorkspaceRoot = workspaceRoot
	}
	mdl := &convergenceModel{
		parentRun:        opts.Snapshot.Run,
		width:            120,
		height:           40,
		cfg:              &localCfg,
		quitDialog:       newQuitDialogState(),
		now:              time.Now(),
		loadConvergence:  opts.LoadConvergence,
		approve:          opts.Approve,
		resume:           opts.Resume,
		cancel:           opts.Cancel,
		newActionContext: defaultConvergenceActionContext,
	}
	if opts.HasConvergence {
		mdl.applyConvergenceSnapshot(opts.Convergence)
	}
	return mdl
}

func newConvergenceController(ctx context.Context, mdl *convergenceModel) remoteWorkerSession {
	if ctx == nil {
		ctx = context.Background()
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	if mdl == nil {
		mdl = newRemoteConvergenceModel(RemoteConvergenceAttachOptions{})
	}
	ctrl := &convergenceController{
		model:  mdl,
		done:   make(chan error, 1),
		ctx:    sessionCtx,
		cancel: cancel,
	}
	mdl.onQuit = ctrl.requestQuit
	mdl.newActionContext = func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(ctrl.ctx, convergenceActionTimeout)
	}
	ctrl.prog = newConvergenceTeaProgram(mdl)
	go func() {
		_, runErr := ctrl.prog.Run()
		if runErr != nil {
			ctrl.done <- runErr
		}
		close(ctrl.done)
	}()
	return ctrl
}

func defaultNewConvergenceTeaProgram(mdl tea.Model) *tea.Program {
	return tea.NewProgram(mdl, tea.WithoutSignalHandler())
}

func defaultConvergenceActionContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), convergenceActionTimeout)
}

func (c *convergenceController) Enqueue(msg any) {
	if c == nil || c.prog == nil {
		return
	}
	c.prog.Send(msg)
}

func (c *convergenceController) SetQuitHandler(fn func(uiQuitRequest)) {
	if c == nil {
		return
	}
	c.quitHandlerMu.Lock()
	defer c.quitHandlerMu.Unlock()
	c.quitHandler = fn
}

func (c *convergenceController) SetJobControlHandler(
	func(context.Context, uiJobControlRequest) (jobControlResponse, error),
) {
	// Convergence phases, rounds, and batches are projections of one daemon-owned
	// run, not embedded job cockpits, so there is no per-job control handler.
}

func (c *convergenceController) requestQuit(req uiQuitRequest) {
	c.quitHandlerMu.RLock()
	fn := c.quitHandler
	c.quitHandlerMu.RUnlock()
	if fn != nil {
		fn(req)
	}
}

func (c *convergenceController) CloseEvents() {}

func (c *convergenceController) Shutdown() {
	if c == nil {
		return
	}
	c.shutdownOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		if c.prog != nil {
			c.prog.Quit()
		}
	})
}

func (c *convergenceController) Wait() error {
	if c == nil {
		return nil
	}
	err, ok := <-c.done
	if c.cancel != nil {
		c.cancel()
	}
	c.workers.Wait()
	if !ok {
		return nil
	}
	return err
}

func (c *convergenceController) StartRemoteWorker(fn func(context.Context)) {
	if c == nil || fn == nil {
		return
	}
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		fn(c.ctx)
	}()
}

// convergenceStreamSession forwards every stream event to the model and signals a
// snapshot reload, so the bounded projection is refreshed from canonical state.
type convergenceStreamSession struct {
	Session
	onEvent func(events.Event)
}

func (s convergenceStreamSession) Enqueue(msg any) {
	s.Session.Enqueue(msg)
	if ev, ok := msg.(events.Event); ok && s.onEvent != nil {
		s.onEvent(ev)
	}
}

func followRemoteConvergence(
	ctx context.Context,
	session Session,
	opts RemoteConvergenceAttachOptions,
	stream apiclient.RunStream,
) {
	followOpts := RemoteAttachOptions{
		LoadSnapshot: func(loadCtx context.Context) (apicore.RunSnapshot, error) {
			if opts.LoadRunSnapshot == nil {
				return opts.Snapshot, nil
			}
			return opts.LoadRunSnapshot(loadCtx)
		},
		OpenStream: opts.OpenStream,
	}
	followRemoteRun(ctx, session, followOpts, stream, apicore.StreamCursor{})
}

func reloadConvergenceSnapshot(
	ctx context.Context,
	session Session,
	load func(context.Context) (contract.ConvergenceSnapshot, error),
) {
	if load == nil || session == nil {
		return
	}
	snapshot, err := load(ctx)
	if err != nil {
		return
	}
	session.Enqueue(convergenceSnapshotMsg{snapshot: snapshot})
}

func (m *convergenceModel) Init() tea.Cmd {
	return m.clockTick()
}

func (m *convergenceModel) clockTick() tea.Cmd {
	return tea.Every(uiClockTickInterval, func(at time.Time) tea.Msg {
		return clockTickMsg{at: at}
	})
}

func (m *convergenceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch value := msg.(type) {
	case tea.KeyPressMsg:
		return m, m.handleKey(value)
	case tea.WindowSizeMsg:
		m.width = value.Width
		m.height = value.Height
		return m, nil
	case clockTickMsg:
		if !value.at.IsZero() {
			m.now = value.at
		}
		m.refreshView()
		return m, m.clockTick()
	case spinnerTickMsg:
		return m, nil
	case convergenceSnapshotMsg:
		m.applyConvergenceSnapshot(value.snapshot)
		return m, nil
	case convergenceActionResultMsg:
		m.handleActionResult(value)
		return m, nil
	case events.Event:
		m.handleParentEvent(value)
		return m, nil
	default:
		return m, nil
	}
}

func (m *convergenceModel) applyConvergenceSnapshot(snapshot contract.ConvergenceSnapshot) {
	m.snapshot = snapshot
	m.hasSnapshot = true
	m.refreshView()
	m.clampScroll()
	if snapshot.Terminal != nil {
		if status := strings.TrimSpace(snapshot.Terminal.Status); status != "" {
			m.parentRun.Status = status
		}
		m.quitDialog.Close()
	}
}

func (m *convergenceModel) refreshView() {
	if !m.hasSnapshot {
		return
	}
	m.view = projectConvergenceView(m.snapshot, m.now)
}

func (m *convergenceModel) handleParentEvent(ev events.Event) {
	switch ev.Kind {
	case events.EventKindRunCompleted:
		m.parentRun.Status = remoteRunStatusCompleted
		m.quitDialog.Close()
	case events.EventKindRunFailed:
		m.parentRun.Status = remoteRunStatusFailed
		m.quitDialog.Close()
	case events.EventKindRunCancelled:
		m.parentRun.Status = remoteRunStatusCanceled
		m.quitDialog.Close()
	default:
	}
}

func (m *convergenceModel) handleActionResult(msg convergenceActionResultMsg) {
	if msg.err != nil {
		m.lastError = fmt.Sprintf("%s failed: %v", convergenceActionLabel(msg.action), msg.err)
		return
	}
	m.lastError = ""
}

// isTerminal reports whether the convergence segment reached a terminal outcome.
// It relies on the projected snapshot, which recognizes parked as terminal, unlike
// the generic run-status terminal check.
func (m *convergenceModel) isTerminal() bool {
	if m.hasSnapshot && m.snapshot.Terminal != nil {
		return true
	}
	return isTerminalRunStatus(m.parentRun.Status)
}
