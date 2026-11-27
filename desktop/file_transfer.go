package main

import (
	"context"
	"errors"
)

const fileTransferProgressEvent = "sftp:transfer-progress"

type StartFileTransferReq struct {
	Source          FileEndpoint    `json:"source"`
	Destination     FileEndpoint    `json:"destination"`
	Paths           []string        `json:"paths"`
	DestinationDir  string          `json:"destination_dir"`
	Conflict        string          `json:"conflict"`
	AcceptedHostKey AcceptedHostKey `json:"accepted_host_key"`
}

func (a *App) StartFileTransfer(req StartFileTransferReq) (string, error) {
	return a.fileTransferManager().Start(fileTransferRequest{
		Source:         req.Source,
		Destination:    req.Destination,
		Paths:          append([]string(nil), req.Paths...),
		DestinationDir: req.DestinationDir,
		Conflict:       transferConflict(req.Conflict),
		AcceptedHostKey: transferHostKeyAcceptance{
			Host:        req.AcceptedHostKey.Host,
			Fingerprint: req.AcceptedHostKey.Fingerprint,
		},
	})
}

func (a *App) CancelFileTransfer(id string) error {
	return a.fileTransferManager().Cancel(id)
}

func (a *App) RetryFileTransfer(id string, accepted AcceptedHostKey) (string, error) {
	return a.fileTransferManager().Retry(id, transferHostKeyAcceptance{
		Host: accepted.Host, Fingerprint: accepted.Fingerprint,
	})
}

func (a *App) ListFileTransfers() []FileTransferJob {
	return a.fileTransferManager().List()
}

func (a *App) fileTransferManager() *fileTransferManager {
	a.fileTransfersMu.Lock()
	defer a.fileTransfersMu.Unlock()
	if a.fileTransfers == nil {
		ctx := a.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		a.fileTransfers = newFileTransferManager(ctx, a.openFileTransferEndpoint, func(job FileTransferJob) {
			if a.eventsEmitter != nil {
				a.eventsEmitter(a.ctx, fileTransferProgressEvent, job)
			}
		})
	}
	return a.fileTransfers
}

func (a *App) openFileTransferEndpoint(ctx context.Context, endpoint FileEndpoint, accepted transferHostKeyAcceptance) (transferEndpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch endpoint.Kind {
	case "local":
		if a.pluginFS == nil || a.pluginFS.access == nil {
			return nil, errors.New("local file access is unavailable")
		}
		return newLocalTransferEndpoint(a.pluginFS.access), nil
	case "ssh":
		return a.newSFTPTransferEndpoint(ctx, endpoint.HostID, AcceptedHostKey{
			Host:        accepted.Host,
			Fingerprint: accepted.Fingerprint,
		})
	default:
		return nil, errors.New("unsupported file transfer endpoint")
	}
}

func (a *App) shutdownFileTransfers() {
	a.fileTransfersMu.Lock()
	manager := a.fileTransfers
	a.fileTransfersMu.Unlock()
	if manager != nil {
		manager.Shutdown()
	}
}
