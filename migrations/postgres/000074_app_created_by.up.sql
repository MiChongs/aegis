-- +migrate Up
-- 应用的创建者。
--
-- 自助注册出来的管理员**没有任何角色分配**，唯一能把自己从"零权限"里带出来的
-- 动作就是「创建属于自己的第一个应用」，成为它的 app_admin。这条路要成立，
-- 平台就必须能回答「这个人自助建了几个应用」—— 否则一个注册接口等于一个
-- 无限建库的入口。
--
-- 为什么不用 admin_assignments 里的 app_admin 条数来数：那记的是"谁在管"，
-- 不是"谁建的"。超管把某人授权成 5 个既有应用的管理员之后，此人的自助配额
-- 就凭空被吃光了 —— 而他一个应用都没建过。两件事必须分开记。
--
-- 同时它也是审计线索：应用列表上「这个应用是谁拉起来的」此前无处可查。
ALTER TABLE apps ADD COLUMN IF NOT EXISTS created_by BIGINT NULL
    REFERENCES admin_accounts(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_apps_created_by ON apps(created_by);

-- 存量回填：把每个应用最早的一条 app_admin 分配当成创建者的证据。
-- 这是**推断**不是事实，但比全部留空好 —— 留空会让升级后所有存量应用的
-- 创建者都算在"无人"名下，于是老用户的自助配额从 0 重新开始计。
-- 分配是超管手工授的（那种情况本就不该占配额）时会误记一次，
-- 影响面是"少建一个应用"，而反过来（漏记）的影响面是配额形同虚设。
UPDATE apps a
SET created_by = first_admin.admin_id
FROM (
    SELECT DISTINCT ON (appid) appid, admin_id
    FROM admin_assignments
    WHERE role_key = 'app_admin' AND appid IS NOT NULL
    ORDER BY appid, created_at ASC, id ASC
) AS first_admin
WHERE a.created_by IS NULL AND a.id = first_admin.appid;
