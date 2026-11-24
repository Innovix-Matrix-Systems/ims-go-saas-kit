package dev

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the developer dashboard JSON API (mutations declare
// dev.manage with r.With(h.manage)). The caller provides
// the authentication, tenancy-exempt and dev.view middleware chain.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/me", h.me)
	r.Get("/overview", h.getOverview)

	r.Get("/scheduler", h.schedulerStatus)
	r.Get("/scheduler/runs", h.listScheduleRuns)
	r.With(h.manage).Post("/scheduler/{id}/run", h.runScheduledTask)
	r.Get("/actions", h.listActions)
	r.Get("/actions/facets", h.actionFacets)
	r.Get("/actions/stats", h.actionStats)
	r.Get("/actions/{id}", h.getAction)
	r.With(h.manage).Post("/simulate-events", h.simulateEvents)

	r.Get("/playground", h.playgroundInfoHandler)
	r.With(h.manage).Post("/playground/jobs", h.enqueuePlaygroundJobs)

	r.Get("/requests", h.listRequests)
	r.Get("/requests/{id}", h.getRequest)
	r.Get("/logs", h.listLogs)
	r.Get("/logs/{id}", h.getLog)
	r.Get("/exceptions", h.listExceptions)
	r.Get("/exceptions/{id}", h.getException)
	r.With(h.manage).Post("/telemetry/prune", h.pruneTelemetry)

	r.Route("/queue", func(r chi.Router) {
		r.Get("/queues", h.listQueues)
		r.With(h.manage).Post("/queues/{name}/pause", h.pauseQueue)
		r.With(h.manage).Post("/queues/{name}/resume", h.resumeQueue)
		r.Get("/jobs", h.listJobs)
		r.Get("/jobs/{id}", h.getJob)
		r.With(h.manage).Post("/jobs/{id}/retry", h.retryJob)
		r.With(h.manage).Post("/jobs/{id}/cancel", h.cancelJob)
		r.With(h.manage).Delete("/jobs/{id}", h.deleteJob)
	})

	r.Get("/sessions/stats", h.sessionStats)
	r.Get("/sessions/users", h.listSessionUsers)
	r.Get("/sessions/users/{id}/tokens", h.listUserSessions)
	r.With(h.manage).Delete("/sessions/tokens/{id}", h.revokeSessionToken)
	r.With(h.manage).Delete("/sessions/users/{id}", h.revokeUserSessions)
	r.With(h.manage).Delete("/sessions", h.revokeAllSessions)
	r.With(h.manage).Post("/sessions/prune", h.pruneSessions)

	r.Get("/developers", h.listDevelopers)
	r.Get("/developers/stats", h.developerStats)
	r.With(h.manage).Post("/developers", h.createDeveloper)
	r.With(h.manage).Post("/developers/role", h.ensureDeveloperRole)
	r.With(h.manage).Post("/developers/role/all", h.grantDeveloperAllPermissions)
	r.With(h.manage).Delete("/developers/{user_id}", h.removeDeveloper)

	r.Get("/files", h.listFiles)
	r.Get("/files/stats", h.fileStats)
	r.With(h.manage).Get("/files/{id}/download", h.downloadFile)
	r.With(h.manage).Delete("/files/{id}", h.deleteFile)

	r.Get("/backups", h.listBackups)
	r.Get("/backups/stats", h.backupStats)
	r.Get("/backups/config", h.backupConfig)
	r.With(h.manage).Post("/backups", h.createBackup)
	r.With(h.manage).Post("/backups/clean", h.cleanBackups)
	r.Get("/backups/{id}", h.getBackup)
	r.With(h.manage).Get("/backups/{id}/download", h.downloadBackup)
	r.With(h.manage).Delete("/backups/{id}", h.deleteBackup)

	r.Get("/targets", h.searchTargets)
	r.Get("/targets/stats", h.targetStats)

	r.Get("/settings/groups", h.listDevSettingGroups)
	r.With(h.manage).Put("/settings/groups/order", h.reorderSettingGroups)
	r.With(h.manage).Put("/settings/groups/{id}/keys", h.placeSettings)
	r.With(h.manage).Post("/settings/groups", h.createDevSettingGroup)
	r.With(h.manage).Patch("/settings/groups/{id}", h.updateDevSettingGroup)
	r.With(h.manage).Delete("/settings/groups/{id}", h.deleteDevSettingGroup)
	r.With(h.manage).Post("/settings/sync", h.syncDevSettings)
	r.Get("/settings", h.listDevSettings)
	r.With(h.manage).Patch("/settings/{key}", h.setDevSetting)
	r.With(h.manage).Delete("/settings/{key}", h.resetDevSetting)
	r.With(h.manage).Patch("/settings/{key}/group", h.assignDevSettingGroup)

	r.Get("/tenants", h.listTenants)
	r.Get("/tenants/{id}", h.getTenant)

	r.Get("/inspector/routes", h.inspectorRoutes)
	r.Get("/inspector/access", h.inspectorAccess)
	r.Get("/inspector/config", h.inspectorConfig)
	r.Get("/inspector/packages", h.inspectorPackages)

	r.Get("/maintenance", h.maintenanceStatus)
	r.With(h.manage).Post("/maintenance/down", h.maintenanceDown)
	r.With(h.manage).Post("/maintenance/up", h.maintenanceUp)

	r.Get("/deliveries", h.listDeliveries)
	r.Get("/deliveries/templates", h.listDeliveryTemplates)
	r.Get("/deliveries/templates/{name}/preview", h.previewDeliveryTemplate)
	r.Get("/deliveries/{id}", h.getDelivery)

	r.Get("/commands", h.listCommands)
	r.Get("/commands/runs", h.listCommandRuns)
	r.Get("/commands/runs/{id}", h.getCommandRun)
	r.With(h.manage).Post("/commands/{name}/run", h.runCommand)

	r.Get("/flags", h.listFlags)
	r.With(h.manage).Patch("/flags", h.updateFlags)
	r.With(h.manage).Post("/flags/sync", h.syncFlags)
	r.With(h.manage).Delete("/flags/{key}", h.resetFlag)
}
