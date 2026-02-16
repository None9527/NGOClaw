package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/repository"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/valueobject"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/persistence/models"
	domainErrors "github.com/ngoclaw/ngoclaw/gateway/pkg/errors"
	"gorm.io/gorm"
)

// GormMessageRepository GORM 实现的消息仓储
type GormMessageRepository struct {
	db *gorm.DB
}

// NewGormMessageRepository 创建 GORM 消息仓储
func NewGormMessageRepository(db *gorm.DB) repository.MessageRepository {
	return &GormMessageRepository{
		db: db,
	}
}

// Save 保存消息
func (r *GormMessageRepository) Save(ctx context.Context, message *entity.Message) error {
	model, err := r.toModel(message)
	if err != nil {
		return err
	}

	// 使用 Save 支持创建或更新
	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return domainErrors.NewInternalError("failed to save message: " + err.Error())
	}

	return nil
}

// FindByID 根据ID查找消息
func (r *GormMessageRepository) FindByID(ctx context.Context, id string) (*entity.Message, error) {
	var model models.MessageModel
	if err := r.db.WithContext(ctx).First(&model, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainErrors.NewNotFoundError("message not found")
		}
		return nil, domainErrors.NewInternalError("failed to find message: " + err.Error())
	}

	return r.toEntity(&model)
}

// FindByConversationID 根据会话ID查找消息列表
func (r *GormMessageRepository) FindByConversationID(ctx context.Context, conversationID string, limit, offset int) ([]*entity.Message, error) {
	var models []models.MessageModel
	err := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("created_at asc").
		Limit(limit).
		Offset(offset).
		Find(&models).Error

	if err != nil {
		return nil, domainErrors.NewInternalError("failed to find messages: " + err.Error())
	}

	messages := make([]*entity.Message, 0, len(models))
	for _, model := range models {
		msg, err := r.toEntity(&model)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// Delete 删除消息
func (r *GormMessageRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Delete(&models.MessageModel{}, "id = ?", id)
	if result.Error != nil {
		return domainErrors.NewInternalError("failed to delete message: " + result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return domainErrors.NewNotFoundError("message not found")
	}
	return nil
}

// Count 统计会话中的消息数量
func (r *GormMessageRepository) Count(ctx context.Context, conversationID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.MessageModel{}).
		Where("conversation_id = ?", conversationID).
		Count(&count).Error

	if err != nil {
		return 0, domainErrors.NewInternalError("failed to count messages: " + err.Error())
	}
	return count, nil
}

// 转换方法

func (r *GormMessageRepository) toModel(entity *entity.Message) (*models.MessageModel, error) {
	// 序列化元数据
	metadataBytes, err := json.Marshal(entity.GetAllMetadata())
	if err != nil {
		return nil, domainErrors.NewInternalError("failed to marshal metadata: " + err.Error())
	}

	return &models.MessageModel{
		ID:             entity.ID(),
		ConversationID: entity.ConversationID(),
		Content:        entity.Content().Text(),
		ContentType:    string(entity.Content().ContentType()),
		SenderID:       entity.Sender().ID(),
		SenderName:     entity.Sender().Username(),
		SenderType:     entity.Sender().Type(),
		CreatedAt:      entity.Timestamp(),
		UpdatedAt:      time.Now(),
		Metadata:       string(metadataBytes),
	}, nil
}

func (r *GormMessageRepository) toEntity(model *models.MessageModel) (*entity.Message, error) {
	content := valueobject.NewMessageContent(model.Content, valueobject.ContentType(model.ContentType))
	sender := valueobject.NewUser(model.SenderID, model.SenderName, model.SenderType)

	var metadata map[string]interface{}
	if model.Metadata != "" {
		if err := json.Unmarshal([]byte(model.Metadata), &metadata); err != nil {
			// 如果元数据解析失败，记录日志但不中断流程
			// log.Warn("Failed to unmarshal metadata", zap.Error(err))
			metadata = make(map[string]interface{})
		}
	} else {
		metadata = make(map[string]interface{})
	}

	msg := entity.ReconstructMessage(
		model.ID,
		model.ConversationID,
		content,
		sender,
		model.CreatedAt,
		metadata,
	)

	return msg, nil
}
