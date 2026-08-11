-- 归一化 risk_assessments 的 JSONB 形状。
--
-- 旧写入路径对 nil 切片 / nil map 调 json.Marshal 得到的是**标量 `null`**
-- 而不是 `[]` / `{}`，列上的 DEFAULT 挡不住显式写入。结果是
-- `jsonb_array_elements(matched_rules)` 在这些行上抛
-- 22023 cannot extract elements from a scalar —— 风控大盘整个 500。
--
-- 代码侧已在写入与查询两处都做了收敛；这里把存量数据一并修正，
-- 并加 CHECK 让「再写进一个标量」在数据库层面就不可能。

UPDATE risk_assessments
SET matched_rules = '[]'::jsonb
WHERE matched_rules IS NULL OR jsonb_typeof(matched_rules) <> 'array';

UPDATE risk_assessments
SET eval_context = '{}'::jsonb
WHERE eval_context IS NULL OR jsonb_typeof(eval_context) <> 'object';

ALTER TABLE risk_assessments DROP CONSTRAINT IF EXISTS chk_risk_assessments_matched_rules_array;
ALTER TABLE risk_assessments ADD CONSTRAINT chk_risk_assessments_matched_rules_array
    CHECK (jsonb_typeof(matched_rules) = 'array');

ALTER TABLE risk_assessments DROP CONSTRAINT IF EXISTS chk_risk_assessments_eval_context_object;
ALTER TABLE risk_assessments ADD CONSTRAINT chk_risk_assessments_eval_context_object
    CHECK (jsonb_typeof(eval_context) = 'object');

-- 同源问题：规则与设备指纹的 JSONB 也可能是标量 null
UPDATE risk_rules SET condition_data = '{}'::jsonb
WHERE condition_data IS NULL OR jsonb_typeof(condition_data) <> 'object';

UPDATE device_fingerprints SET fingerprint = '{}'::jsonb
WHERE fingerprint IS NULL OR jsonb_typeof(fingerprint) <> 'object';
