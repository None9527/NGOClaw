package telegram

// TelegramMessageLimit Telegram 消息长度限制
const TelegramMessageLimit = 4096

// ChunkMessage 分块消息 (超过 4096 字符)
// 参考 OpenClaw draft-chunking.ts
func ChunkMessage(text string) []string {
	if len(text) <= TelegramMessageLimit {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= TelegramMessageLimit {
			chunks = append(chunks, remaining)
			break
		}

		// 在段落/句子边界分割
		splitIndex := findSplitPoint(remaining, TelegramMessageLimit)
		if splitIndex <= 0 {
			splitIndex = TelegramMessageLimit
		}

		chunks = append(chunks, remaining[:splitIndex])
		remaining = trimLeft(remaining[splitIndex:])
	}

	return chunks
}

// findSplitPoint 寻找分割点
// 优先级: 双换行 > 单换行 > 句号 > 空格 > 强制截断
func findSplitPoint(text string, maxLen int) int {
	// 1. 尝试在双换行处分割 (段落边界)
	idx := lastIndexOf(text, "\n\n", maxLen)
	if idx >= maxLen/2 {
		return idx
	}

	// 2. 尝试在单换行处分割
	idx = lastIndexOf(text, "\n", maxLen)
	if idx >= maxLen/2 {
		return idx
	}

	// 3. 尝试在句号处分割
	idx = lastIndexOfAny(text, []string{". ", "。", "！", "？"}, maxLen)
	if idx >= maxLen/2 {
		return idx + 1 // 包含标点
	}

	// 4. 尝试在空格处分割
	idx = lastIndexOf(text, " ", maxLen)
	if idx >= maxLen/3 {
		return idx
	}

	// 5. 强制截断
	return maxLen
}

// lastIndexOf 从末尾查找子串
func lastIndexOf(s, substr string, maxPos int) int {
	if maxPos > len(s) {
		maxPos = len(s)
	}
	searchArea := s[:maxPos]
	
	for i := len(searchArea) - len(substr); i >= 0; i-- {
		if searchArea[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// lastIndexOfAny 从末尾查找任意子串
func lastIndexOfAny(s string, substrs []string, maxPos int) int {
	if maxPos > len(s) {
		maxPos = len(s)
	}
	searchArea := s[:maxPos]

	for i := len(searchArea) - 1; i >= 0; i-- {
		for _, substr := range substrs {
			if i+len(substr) <= len(searchArea) {
				if searchArea[i:i+len(substr)] == substr {
					return i
				}
			}
		}
	}
	return -1
}

// trimLeft 去除左侧空白
func trimLeft(s string) string {
	start := 0
	for start < len(s) {
		c := s[start]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			start++
		} else {
			break
		}
	}
	return s[start:]
}

// ChunkMarkdown 分块 Markdown 文本 (保持代码块完整)
func ChunkMarkdown(text string) []string {
	if len(text) <= TelegramMessageLimit {
		return []string{text}
	}

	// Find all code block boundaries
	type codeBlock struct{ start, end int }
	var blocks []codeBlock
	i := 0
	for i < len(text) {
		if i+2 < len(text) && text[i:i+3] == "```" {
			start := i
			// Find closing ```
			j := i + 3
			for j < len(text) {
				if j+2 < len(text) && text[j:j+3] == "```" {
					blocks = append(blocks, codeBlock{start, j + 3})
					i = j + 3
					break
				}
				j++
			}
			if j >= len(text) {
				// Unclosed code block — treat rest as code
				blocks = append(blocks, codeBlock{start, len(text)})
				break
			}
		} else {
			i++
		}
	}

	// Split with code block awareness
	var chunks []string
	remaining := text
	offset := 0

	for len(remaining) > 0 {
		if len(remaining) <= TelegramMessageLimit {
			chunks = append(chunks, remaining)
			break
		}

		splitAt := TelegramMessageLimit

		// Check if split point falls inside a code block
		absPos := offset + splitAt
		for _, blk := range blocks {
			if absPos > blk.start && absPos < blk.end {
				// Split falls inside a code block
				if blk.start-offset > TelegramMessageLimit/3 {
					// Move split before the code block
					splitAt = blk.start - offset
				} else if blk.end-offset <= TelegramMessageLimit*2 {
					// Keep entire code block — allow slight overshoot
					splitAt = blk.end - offset
				}
				break
			}
		}

		// Fine-tune split at paragraph/sentence boundary
		if splitAt >= TelegramMessageLimit {
			splitAt = findSplitPoint(remaining, TelegramMessageLimit)
			if splitAt <= 0 {
				splitAt = TelegramMessageLimit
			}
		}

		chunk := remaining[:splitAt]
		chunks = append(chunks, fixTruncatedCodeBlock(chunk))
		remaining = trimLeft(remaining[splitAt:])
		offset += splitAt
	}

	return chunks
}

// fixTruncatedCodeBlock 修复截断的代码块
func fixTruncatedCodeBlock(chunk string) string {
	// 计算反引号对
	backtickCount := 0
	inCodeBlock := false
	
	for i := 0; i < len(chunk); i++ {
		if i+2 < len(chunk) && chunk[i:i+3] == "```" {
			inCodeBlock = !inCodeBlock
			backtickCount++
			i += 2
		}
	}
	
	// 如果在代码块中结束，添加闭合
	if inCodeBlock {
		chunk += "\n```"
	}
	
	return chunk
}

// SendChunkedMessage 发送分块消息
func (a *Adapter) SendChunkedMessage(chatID int64, text string, parseMode string) error {
	chunks := ChunkMessage(text)
	
	for _, chunk := range chunks {
		msg := &OutgoingMessage{
			ChatID:    chatID,
			Text:      chunk,
			ParseMode: parseMode,
		}
		if err := a.SendMessage(msg); err != nil {
			return err
		}
	}
	
	return nil
}
