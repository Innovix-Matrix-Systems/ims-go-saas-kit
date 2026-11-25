import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import type { DevDatabasePool, DevDatabasePools } from '../api/types'
import { formatDuration, relativeTime } from '../format'
import { Badge, Card, ErrorBox, LoadingBlock } from './ui'

/**
 * The database connection pools, read live from /dev/api/inspector/database.
 * The counts are dashboard-only: the public /api/health report names a busy
 * pool but never says how many connections it has.
 */
export function useDatabasePools(refetchInterval: number | false = 10_000) {
  return useQuery({
    queryKey: ['inspector', 'database'],
    queryFn: ({ signal }) => api<DevDatabasePools>('/dev/api/inspector/database', { signal }),
    refetchInterval,
    retry: false,
  })
}

/** "3/20 active" for one pool, or for several "primary 3/20 · replica 1/10". */
export function poolsSummary(pools: DevDatabasePool[]): string {
  if (pools.length === 1) return `${pools[0].active}/${pools[0].max} active`
  return pools.map((pool) => `${pool.name} ${pool.active}/${pool.max}`).join(' · ')
}

/** Inspector → System: one row per pool with a usage bar. */
export function DatabasePoolsCard() {
  const pools = useDatabasePools(5_000)
  const data = pools.data
  return (
    <Card
      className="mt-16"
      title="Database pools"
      description={
        data
          ? `Live · checked ${relativeTime(data.checked_at)} · busy at ${Math.round(data.busy_share * 100)}% of max`
          : 'Connections in use, per pool'
      }
      flush
    >
      {pools.isLoading ? (
        <LoadingBlock label="Reading pools…" />
      ) : pools.isError ? (
        <ErrorBox message={pools.error instanceof Error ? pools.error.message : 'Unable to read the pools.'} onRetry={() => void pools.refetch()} />
      ) : !data || data.pools.length === 0 ? (
        <div className="card-body small dim">No database connections configured.</div>
      ) : (
        <table className="table pool-table">
          <thead>
            <tr>
              <th>Pool</th>
              <th>In use</th>
              <th className="num">Idle</th>
              <th className="num">Opening</th>
              <th className="num">Waited</th>
              <th className="num">Avg. acquire</th>
            </tr>
          </thead>
          <tbody>
            {data.pools.map((pool) => (
              <PoolRow key={pool.name} pool={pool} />
            ))}
          </tbody>
        </table>
      )}
    </Card>
  )
}

function PoolRow({ pool }: { pool: DevDatabasePool }) {
  const share = pool.max > 0 ? Math.min(1, pool.active / pool.max) : 0
  const waitedShare = pool.acquires > 0 ? pool.waited / pool.acquires : 0
  return (
    <tr>
      <td>
        <span className="mono">{pool.name}</span> {pool.busy ? <Badge tone="warn">busy</Badge> : null}
      </td>
      <td>
        <div className="pool-usage">
          <span
            className={`pool-meter ${pool.busy ? 'busy' : ''}`}
            role="meter"
            aria-label={`${pool.name} connections in use`}
            aria-valuemin={0}
            aria-valuemax={pool.max}
            aria-valuenow={pool.active}
          >
            <span style={{ width: `${share * 100}%` }} />
          </span>
          <span className="mono small">
            {pool.active}/{pool.max}
          </span>
        </div>
      </td>
      <td className="num mono">{pool.idle}</td>
      <td className="num mono">{pool.constructing}</td>
      <td className="num mono" title={`${pool.waited} of ${pool.acquires} acquires waited, ${pool.canceled} gave up`}>
        {pool.waited} <span className="dim">({(waitedShare * 100).toFixed(waitedShare > 0 && waitedShare < 0.01 ? 2 : 0)}%)</span>
      </td>
      <td className="num mono">{formatDuration(pool.avg_acquire_ms)}</td>
    </tr>
  )
}
