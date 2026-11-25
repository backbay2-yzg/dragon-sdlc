package pipeline

import "dragon/internal/store"

// EventSink emits real-time pipeline events to the desktop UI.
type EventSink interface {
	Emit(eventName string, payload any)
}

// Notifier dispatches Windows Toast notifications for key pipeline checkpoints.
type Notifier interface {
	NotifyApprovalNeeded(run store.PipelineRun, stage store.StageExecution)
	NotifyRunFailed(run store.PipelineRun, errMsg string)
	NotifyRunSucceeded(run store.PipelineRun)
}

// TokenResolver supplies the active GitHub token for git push/fetch operations.
type TokenResolver func() string
