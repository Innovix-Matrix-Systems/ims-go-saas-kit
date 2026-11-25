import type { components } from './schema'

export type DevAction = components['schemas']['DevAction']
export type DevQueue = components['schemas']['DevQueue']
export type DevJob = components['schemas']['DevJob']
export type DevRequestSummary = components['schemas']['DevRequestSummary']
export type DevRequestRecord = components['schemas']['DevRequest']
export type DevRequestDetail = components['schemas']['DevRequestDetail']
export type DevLog = components['schemas']['DevLog']
export type DevException = components['schemas']['DevException']
export type DevQuery = components['schemas']['DevQuery']
export type DevCacheEvent = components['schemas']['DevCacheEvent']
export type DevQueueEvent = components['schemas']['DevQueueEvent']
export type DevSessionStats = components['schemas']['DevSessionStats']
export type DevSessionUser = components['schemas']['DevSessionUser']
export type DevSessionToken = components['schemas']['DevSessionToken']
export type DevDeveloper = components['schemas']['DevDeveloper']
export type DevDeveloperPage = components['schemas']['DevDeveloperPageEnvelope']
export type DevDeveloperStats = components['schemas']['DevDeveloperStats']
export type DevTarget = components['schemas']['DevTarget']
export type DevTargetStats = components['schemas']['DevTargetStats']
export type DevTargetPage = components['schemas']['DevTargetPageEnvelope']
export type DevFile = components['schemas']['DevFile']
export type DevFilePage = components['schemas']['DevFilePageEnvelope']
export type DevFileStats = components['schemas']['DevFileStats']
export type DevBackupRun = components['schemas']['DevBackupRun']
export type DevBackupList = components['schemas']['DevBackupListEnvelope']
export type DevBackupStats = components['schemas']['DevBackupStats']
export type DevOverview = components['schemas']['DevOverview']
export type DevScheduler = components['schemas']['DevScheduler']
export type DevScheduledTask = components['schemas']['DevScheduledTask']
export type DevScheduleRun = components['schemas']['DevScheduleRun']
export type DevTrafficMinute = components['schemas']['DevTrafficMinute']
export type DevBackupConfig = components['schemas']['DevBackupConfig']
export type DevBackupCleanResult = components['schemas']['DevBackupCleanEnvelope']['data']
export type DevMaintenance = components['schemas']['DevMaintenanceState']
export type DevDelivery = components['schemas']['DevDelivery']
export type DevDeliveryTemplates = components['schemas']['DevDeliveryTemplates']
export type DevDeliveryContent = components['schemas']['DevDeliveryContent']
export interface SettingsGroup {
  id: number
  slug: string
  name: string
  description?: string
  sort_order: number
  is_system: boolean
}

export interface SettingsItem {
  id: number
  key: string
  type: 'string' | 'int' | 'bool' | 'json'
  value: unknown
  default: unknown
  description: string
  source: 'default' | 'global' | 'tenant'
  scope: 'global' | 'tenant'
  editable: boolean
  overridden: boolean
  sort_order: number
  group: SettingsGroup
}
export type DevFlag = components['schemas']['DevFlag']
export type PageMeta = components['schemas']['PaginationMeta']

export interface Page<T> {
  data: T[]
  links: components['schemas']['PaginationLinks']
  meta: PageMeta
}

export interface DevSessionRevokeResult {
  revoked: number
  excluded_token_id?: number
}

export interface DevSessionPruneResult {
  idle_days: number
  dry_run: boolean
  affected: number
}

export interface Me {
  user_id: number
  name: string
  email: string
  tenant_id?: number | null
  branch_id?: number | null
  is_super_admin: boolean
  playground: boolean
  permissions: string[]
}

export interface PlaygroundInfo {
  enabled: boolean
  queue_driver: string
  job_kind: string
  queues: string[]
  jobs: string[]
}

export interface PlaygroundQueueResult {
  enqueued: number
  kind: string
  queue: string
  driver: string
  fail_on_purpose?: boolean
  processing_job_id?: number
  job_id?: string
}

export interface CursorPage<T> {
  data: T[]
  meta?: { next_cursor?: string }
  links?: unknown
}

export interface LoginResult {
  status: string
  token?: string
  message?: string
  /** The second factor an OTP_REQUIRED sign-in waits for. */
  method?: 'totp' | 'email'
  user?: {
    id: number
    name: string
    email: string
    roles?: { id: number; name: string }[]
  }
}

export interface HealthCheck {
  name: string
  label: string
  status: 'ok' | 'warning' | 'failed' | 'crashed'
  shortSummary: string
  notificationMessage?: string
  meta?: Record<string, unknown>
}

export interface HealthResults {
  finishedAt: number
  checkResults: HealthCheck[]
}

export interface SystemInfo {
  name: string
  environment: string
  version: string
  timezone: string
  go_version?: string
  started_at?: string
  uptime_seconds?: number
}
export type DevRouteReport = components['schemas']['DevRouteReport']
export type DevRoute = DevRouteReport['routes'][number]
export type DevAccessReport = components['schemas']['DevAccessReport']
export type DevConfigSection = components['schemas']['DevConfigSection']
export type DevRequestStats = components['schemas']['DevRequestStats']
export type DevExceptionStats = components['schemas']['DevExceptionStats']
export type DevDeliveryStats = components['schemas']['DevDeliveryStats']
export type DevDatabasePools = components['schemas']['DevDatabasePools']
export type DevDatabasePool = components['schemas']['DevDatabasePool']
export type DevPackagesReport = components['schemas']['DevPackagesReport']
export type DevTranslationsReport = components['schemas']['DevTranslationsReport']
export type DevCommand = components['schemas']['DevCommand']
export type DevCommandOption = DevCommand['options'][number]
export type DevCommandRun = components['schemas']['DevCommandRun']
export type DevTenant = components['schemas']['DevTenant']
export type DevTenantDetail = components['schemas']['DevTenantDetail']
export type DevActionFacets = components['schemas']['DevActionFacets']
export type DevActionStats = components['schemas']['DevActionStats']
