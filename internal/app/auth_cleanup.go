package app

import (
	"context"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/rs/zerolog/log"
)

const authRegistryCleanupInterval = 1 * time.Hour

func (a *App) startAuthRegistryCleanup(ctx context.Context) {
	if a.authRegistry == nil {
		return
	}

	go func() {
		ticker := time.NewTicker(authRegistryCleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.pruneAuthRegistry("scheduled")
			}
		}
	}()
}

func (a *App) pruneAuthRegistry(reason string) {
	if a.authRegistry == nil {
		return
	}

	now := time.Now().UTC()
	expiredDevices, orphaned, err := a.authRegistry.PruneExpired(now)
	if err != nil {
		log.Warn().Err(err).Msg("auth registry prune failed")
		return
	}

	if len(expiredDevices) > 0 {
		log.Info().
			Str("reason", reason).
			Int("expired_devices", len(expiredDevices)).
			Msg("expired device tokens pruned")
	}

	if len(orphaned) > 0 {
		a.cleanupOrphanedWorkspaces(orphaned, "refresh token expired")
	}
}

func (a *App) cleanupOrphanedWorkspaces(workspaceIDs []string, reason string) {
	if a.workspaceConfigManager == nil || a.sessionManager == nil {
		return
	}

	for _, workspaceID := range workspaceIDs {
		ws, err := a.workspaceConfigManager.GetWorkspace(workspaceID)
		if err != nil {
			continue
		}

		if a.sessionManager.CountActiveSessionsForWorkspace(workspaceID) > 0 {
			log.Info().
				Str("workspace_id", workspaceID).
				Str("reason", reason).
				Msg("workspace retained (active sessions present)")
			continue
		}

		if err := a.sessionManager.UnregisterWorkspace(workspaceID); err != nil {
			log.Warn().
				Err(err).
				Str("workspace_id", workspaceID).
				Msg("failed to unregister workspace")
			continue
		}

		if err := a.workspaceConfigManager.RemoveWorkspace(workspaceID); err != nil {
			log.Warn().
				Err(err).
				Str("workspace_id", workspaceID).
				Msg("failed to remove workspace")
			continue
		}

		if a.hub != nil {
			event := events.NewWorkspaceRemovedEvent(ws.Definition.ID, ws.Definition.Name, ws.Definition.Path)
			a.hub.Publish(event)
		}

		log.Info().
			Str("workspace_id", workspaceID).
			Str("reason", reason).
			Msg("workspace removed (no active device tokens)")
	}
}
