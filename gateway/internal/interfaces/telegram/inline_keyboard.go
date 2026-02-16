package telegram

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// InlineButton 内联按钮
type InlineButton struct {
	Text         string
	CallbackData string
}

// BuildInlineKeyboard 构建内联键盘
func BuildInlineKeyboard(rows [][]InlineButton) tgbotapi.InlineKeyboardMarkup {
	keyboard := make([][]tgbotapi.InlineKeyboardButton, len(rows))
	for i, row := range rows {
		keyboard[i] = make([]tgbotapi.InlineKeyboardButton, len(row))
		for j, btn := range row {
			// Telegram 回调数据限制 64 字节
			callbackData := btn.CallbackData
			if len(callbackData) > 64 {
				callbackData = callbackData[:64]
			}
			keyboard[i][j] = tgbotapi.NewInlineKeyboardButtonData(btn.Text, callbackData)
		}
	}
	return tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboard}
}

// BuildProviderKeyboard 构建提供商选择键盘
func BuildProviderKeyboard(providers []string) tgbotapi.InlineKeyboardMarkup {
	var rows [][]InlineButton

	// 每行 2 个按钮
	for i := 0; i < len(providers); i += 2 {
		row := []InlineButton{
			{Text: providers[i], CallbackData: "/models " + providers[i]},
		}
		if i+1 < len(providers) {
			row = append(row, InlineButton{
				Text:         providers[i+1],
				CallbackData: "/models " + providers[i+1],
			})
		}
		rows = append(rows, row)
	}

	return BuildInlineKeyboard(rows)
}

// BuildModelsKeyboard 构建模型选择键盘
func BuildModelsKeyboard(provider string, models []ModelInfo, currentModel string, page, pageSize int) tgbotapi.InlineKeyboardMarkup {
	var rows [][]InlineButton

	// 计算分页
	start := page * pageSize
	end := start + pageSize
	if end > len(models) {
		end = len(models)
	}
	pageModels := models[start:end]
	totalPages := (len(models) + pageSize - 1) / pageSize

	// 模型按钮 (每行 2 个)
	for i := 0; i < len(pageModels); i += 2 {
		row := []InlineButton{}
		for j := 0; j < 2 && i+j < len(pageModels); j++ {
			m := pageModels[i+j]
			text := m.Alias
			if text == "" {
				// 使用 ID 的最后部分
				parts := splitString(m.ID, "/")
				text = parts[len(parts)-1]
			}
			// 标记当前模型
			if m.ID == currentModel {
				text = "✓ " + text
			}
			row = append(row, InlineButton{
				Text:         text,
				CallbackData: "/_setmodel " + m.ID,
			})
		}
		rows = append(rows, row)
	}

	// 分页导航
	if totalPages > 1 {
		navRow := []InlineButton{}
		if page > 0 {
			navRow = append(navRow, InlineButton{
				Text:         "◀️",
				CallbackData: "/models " + provider + " " + intToStr(page-1),
			})
		}
		navRow = append(navRow, InlineButton{
			Text:         intToStr(page+1) + "/" + intToStr(totalPages),
			CallbackData: "noop",
		})
		if page < totalPages-1 {
			navRow = append(navRow, InlineButton{
				Text:         "▶️",
				CallbackData: "/models " + provider + " " + intToStr(page+1),
			})
		}
		rows = append(rows, navRow)
	}

	// 返回按钮
	rows = append(rows, []InlineButton{
		{Text: "← 返回", CallbackData: "/models"},
	})

	return BuildInlineKeyboard(rows)
}

// BuildConfirmKeyboard 构建确认键盘
func BuildConfirmKeyboard(confirmData, cancelData string) tgbotapi.InlineKeyboardMarkup {
	return BuildInlineKeyboard([][]InlineButton{
		{
			{Text: "✅ 确认", CallbackData: confirmData},
			{Text: "❌ 取消", CallbackData: cancelData},
		},
	})
}

// splitString 分割字符串
func splitString(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
		}
	}
	result = append(result, s[start:])
	return result
}

// intToStr 整数转字符串
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	var result []byte
	negative := n < 0
	if negative {
		n = -n
	}
	for n > 0 {
		result = append([]byte{byte('0' + n%10)}, result...)
		n /= 10
	}
	if negative {
		result = append([]byte{'-'}, result...)
	}
	return string(result)
}
