-- 多副本部署的协调表（见 internal/cluster 与 docs/distributed-deploy.md）。
-- schema_migrations 由迁移器自己在执行任何迁移之前建好，这里只建集群租约表；
-- 选主器启动时同样会 CREATE TABLE IF NOT EXISTS，两边定义必须一致。
CREATE TABLE IF NOT EXISTS cluster_leases (
    name        TEXT PRIMARY KEY,
    holder      TEXT NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    renewed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);

COMMENT ON TABLE cluster_leases IS '后台任务选主租约：每个任务名一行，持有者每 TTL/3 续期，过期后由其他实例接手';
