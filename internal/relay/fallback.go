package relay

import (
	"sort"

	"github.com/bestruirui/octopus/internal/price"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
)

// maxFallbackModelsPerChannel 每个渠道兜底尝试的模型数量上限。
// 用于控制成本和重试次数，避免在异常渠道上无限尝试。
const maxFallbackModelsPerChannel = 3

// selectFallbackModels 在指定渠道的可用模型中，选取按上下文长度（能力）降序排列的兜底候选。
//
// 规则：
//   - 只从 channel.Model（渠道在上游真实支持的模型列表）中选取；
//     不使用 channel.CustomModel（别名正是导致上游 404 的常见根因）。
//   - 排除 usedModels 中本轮已尝试过的模型（避免重复）。
//   - 排除 balancer.IsTripped 已熔断的 (channel,model) 组合（模型级熔断，不误伤同渠道其他模型）。
//   - 按 price.GetLLMPrice(modelName).ContextLength 降序排列，context 越大能力越强；无价格数据视为 0（排最后）。
//   - 最多返回 maxFallbackModelsPerChannel 个候选。
func selectFallbackModels(usedModels map[string]bool, channelID, keyID int, channelModels ...string) []string {
	if len(channelModels) == 0 {
		return nil
	}

	type candidate struct {
		name          string
		contextLength int
	}
	candidates := make([]candidate, 0, len(channelModels))

	for _, m := range channelModels {
		if m == "" {
			continue
		}
		if usedModels[m] {
			continue
		}
		if tripped, _ := balancer.IsTripped(channelID, keyID, m); tripped {
			continue
		}
		ctxLen := 0
		if p := price.GetLLMPrice(m); p != nil {
			ctxLen = p.ContextLength
		}
		candidates = append(candidates, candidate{name: m, contextLength: ctxLen})
	}

	if len(candidates) == 0 {
		return nil
	}

	// 按上下文长度降序：能力强的优先兜底
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].contextLength > candidates[j].contextLength
	})

	limit := len(candidates)
	if limit > maxFallbackModelsPerChannel {
		limit = maxFallbackModelsPerChannel
	}

	result := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		result = append(result, candidates[i].name)
	}
	return result
}

// collectUsedModels 从已记录的尝试中收集本轮已经用过的模型名，用于兜底去重。
func collectUsedModels(attemptModelNames []string, groupItemModels []string) map[string]bool {
	used := make(map[string]bool, len(attemptModelNames)+len(groupItemModels))
	for _, m := range attemptModelNames {
		used[m] = true
	}
	// 组内已配置的候选模型也视为“已用过”，兜底只尝试这些之外的
	for _, m := range groupItemModels {
		used[m] = true
	}
	return used
}

// channelModelList 从渠道的 Model 字段（逗号分隔）解析出可用模型列表。
func channelModelList(channelModelCSV string) []string {
	return xstrings.SplitTrimCompact(",", channelModelCSV)
}
