package valueobject

// MessageContent 消息内容值对象（不可变）
type MessageContent struct {
	text        string
	contentType ContentType
	attachments []Attachment
}

// ContentType 内容类型
type ContentType string

const (
	ContentTypeText  ContentType = "text"
	ContentTypeImage ContentType = "image"
	ContentTypeAudio ContentType = "audio"
	ContentTypeVideo ContentType = "video"
	ContentTypeFile  ContentType = "file"
)

// Attachment 附件
type Attachment struct {
	URL      string
	MimeType string
	Size     int64
}

// NewMessageContent 创建消息内容值对象
func NewMessageContent(text string, contentType ContentType) MessageContent {
	return MessageContent{
		text:        text,
		contentType: contentType,
		attachments: make([]Attachment, 0),
	}
}

// NewMessageContentWithAttachments 创建带附件的消息内容
func NewMessageContentWithAttachments(text string, contentType ContentType, attachments []Attachment) MessageContent {
	// 值对象不可变，创建副本
	atts := make([]Attachment, len(attachments))
	copy(atts, attachments)

	return MessageContent{
		text:        text,
		contentType: contentType,
		attachments: atts,
	}
}

// Text 返回文本内容
func (mc MessageContent) Text() string {
	return mc.text
}

// ContentType 返回内容类型
func (mc MessageContent) ContentType() ContentType {
	return mc.contentType
}

// Attachments 返回附件列表（副本）
func (mc MessageContent) Attachments() []Attachment {
	atts := make([]Attachment, len(mc.attachments))
	copy(atts, mc.attachments)
	return atts
}

// HasAttachments 判断是否有附件
func (mc MessageContent) HasAttachments() bool {
	return len(mc.attachments) > 0
}

// IsTextOnly 判断是否仅包含文本
func (mc MessageContent) IsTextOnly() bool {
	return mc.contentType == ContentTypeText && !mc.HasAttachments()
}

// Equals 值对象相等性比较
func (mc MessageContent) Equals(other MessageContent) bool {
	if mc.text != other.text || mc.contentType != other.contentType {
		return false
	}

	if len(mc.attachments) != len(other.attachments) {
		return false
	}

	for i, att := range mc.attachments {
		if att != other.attachments[i] {
			return false
		}
	}

	return true
}
