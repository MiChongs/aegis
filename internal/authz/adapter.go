package authz

import (
	"context"
	"errors"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
)

// policyAdapter 把 authz_policies 表接进 Casbin。
//
// 只实现读，写一律走 Engine 的方法。原因不是省事：Casbin 的 AutoSave 接口
// （AddPolicy/RemovePolicy）**传不了 source 与 owner**，而这两列决定了
// 「这行策略归谁管、重启时会不会被内置定义盖掉」。经它写进去的行没有归属，
// 下次重刷内置策略时既不会被更新也不会被清理，最后沉淀成谁也不敢删的幽灵授权。
type policyAdapter struct {
	store Store
}

var errAdapterReadOnly = errors.New("authz: 策略写入必须走 Engine 的方法（需要记录 source/owner/updated_by）")

var _ persist.Adapter = (*policyAdapter)(nil)

// LoadPolicy 装载全部策略。
func (a *policyAdapter) LoadPolicy(m model.Model) error {
	if a.store == nil {
		return nil
	}
	rules, err := a.store.ListAuthzPolicies(context.Background())
	if err != nil {
		return err
	}
	for _, rule := range rules {
		values := rule.Values
		// 兼容只有三列的历史行：eft 缺省即 allow。
		// 缺省值必须显式补上 —— Casbin 对 `p` 段是按列数匹配的，
		// 少一列的行不会报错，只会永远匹配不上，表现为"配了但不生效"。
		if rule.PType == "p" && len(values) == 3 {
			values = append(values, EffectAllow)
		}
		if len(values) == 0 {
			continue
		}
		line := append([]string{rule.PType}, values...)
		if err := persist.LoadPolicyArray(line, m); err != nil {
			return err
		}
	}
	return nil
}

func (a *policyAdapter) SavePolicy(model.Model) error { return errAdapterReadOnly }

func (a *policyAdapter) AddPolicy(string, string, []string) error { return errAdapterReadOnly }

func (a *policyAdapter) RemovePolicy(string, string, []string) error { return errAdapterReadOnly }

func (a *policyAdapter) RemoveFilteredPolicy(string, string, int, ...string) error {
	return errAdapterReadOnly
}
