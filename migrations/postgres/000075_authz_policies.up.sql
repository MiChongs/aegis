-- +migrate Up
-- 授权策略落库。
--
-- 此前平台有**两个 Casbin enforcer、两套模型、都只活在内存里**：
-- 平台 RBAC 在 AdminService，组织 RBAC 在 OrgAccessControl。两者的策略都在
-- 进程启动时由 Go 代码现拼，于是
--   1. 改一次角色只有处理那次请求的实例知道，多实例部署下"时灵时不灵"；
--   2. 内置角色的权限写死在二进制里，部署方一个字都改不了；
--   3. 没有任何审计线索 —— 谁在什么时候给哪个角色加了什么权限，无处可查。
--
-- 这张表是唯一的策略事实源，Casbin 通过 adapter 读它。
CREATE TABLE IF NOT EXISTS authz_policies (
    id         BIGSERIAL PRIMARY KEY,
    -- Casbin 策略段：p = 权限策略，g = 角色继承边。
    ptype      VARCHAR(8)   NOT NULL,
    -- p: [sub, dom, obj, eft]；g: [child, parent]。
    -- 用定长 6 列而不是数组，是为了让唯一索引挡住重复行 ——
    -- 同一条策略进两遍不会报错，只会让"删掉一条"删不干净。
    v0         VARCHAR(191) NOT NULL DEFAULT '',
    v1         VARCHAR(191) NOT NULL DEFAULT '',
    v2         VARCHAR(191) NOT NULL DEFAULT '',
    v3         VARCHAR(191) NOT NULL DEFAULT '',
    v4         VARCHAR(191) NOT NULL DEFAULT '',
    v5         VARCHAR(191) NOT NULL DEFAULT '',
    -- 归属来源，决定这组策略归谁管：
    --   builtin  内置角色定义，**每次启动按代码整组重刷**（升级能propagate）
    --   custom   自定义角色定义，由角色 CRUD 维护
    --   override 对任意角色（含内置）的人工增减，启动时不动
    --   grant    直接授予/禁止到某个管理员
    --   org      组织角色定义
    -- 少了这一列就无法区分"代码给的"和"人配的"，重刷内置策略时
    -- 只能要么不敢删（残留过期授权）、要么全删（抹掉人工配置）。
    source     VARCHAR(16)  NOT NULL DEFAULT 'custom',
    -- 归属键（角色主体 / 管理员主体）。整组替换的粒度就是 (source, owner)。
    owner      VARCHAR(191) NOT NULL DEFAULT '',
    note       TEXT         NOT NULL DEFAULT '',
    updated_by BIGINT       NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT authz_policies_ptype_chk  CHECK (ptype IN ('p', 'g')),
    CONSTRAINT authz_policies_source_chk CHECK (source IN ('builtin', 'custom', 'override', 'grant', 'org'))
);

-- 同一条策略只能有一行。带 source 是有意的：内置定义与人工 override 可以
-- 长得一模一样（比如 override 里重复声明了一条内置已有的 allow），
-- 那是两条独立的记录，重刷 builtin 时不该把人工那条一起带走。
CREATE UNIQUE INDEX IF NOT EXISTS idx_authz_policies_unique
    ON authz_policies (ptype, source, v0, v1, v2, v3, v4, v5);

-- 整组替换按 (source, owner) 定位。
CREATE INDEX IF NOT EXISTS idx_authz_policies_group ON authz_policies (source, owner);
-- 判定侧只装载全表，但管理端要按主体查"这个角色/这个人有哪些策略"。
CREATE INDEX IF NOT EXISTS idx_authz_policies_subject ON authz_policies (v0);

-- 存量自定义角色的权限迁移进来（admin_role_permissions 仍是角色编辑的展示来源，
-- 判定改读本表）。域取 '*'：角色的权限集与域无关，作用域由 admin_assignments
-- 上的 appid 决定 —— 那份绑定关系每次请求现查，放进缓存化的策略表会引入
-- "撤销了角色但还能用一段时间"的窗口。
INSERT INTO authz_policies (ptype, v0, v1, v2, v3, source, owner)
SELECT 'p', 'role:' || rp.role_key, '*', rp.permission, 'allow', 'custom', 'role:' || rp.role_key
FROM admin_role_permissions rp
WHERE EXISTS (SELECT 1 FROM admin_roles r WHERE r.role_key = rp.role_key)
ON CONFLICT DO NOTHING;

-- base_role 终于有了执行点。这一列从建表起就在，此前只被拿去画角色关系图，
-- 判定完全不看它 —— 于是控制台上标着「继承自应用运营管理员」的自定义角色，
-- 实际一个继承来的权限都没有。
INSERT INTO authz_policies (ptype, v0, v1, source, owner)
SELECT 'g', 'role:' || r.role_key, 'role:' || r.base_role, 'custom', 'role:' || r.role_key
FROM admin_roles r
WHERE r.base_role IS NOT NULL AND btrim(r.base_role) <> '' AND r.base_role <> r.role_key
ON CONFLICT DO NOTHING;
