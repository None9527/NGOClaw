package entity

import "errors"

var (
	// Agent errors
	ErrInvalidAgentID      = errors.New("invalid agent id")
	ErrInvalidAgentName    = errors.New("invalid agent name")
	ErrSkillAlreadyExists  = errors.New("skill already exists")
	ErrSkillNotFound       = errors.New("skill not found")

	// Message errors
	ErrInvalidMessageID      = errors.New("invalid message id")
	ErrInvalidConversationID = errors.New("invalid conversation id")

	// Skill errors
	ErrInvalidSkillID   = errors.New("invalid skill id")
	ErrInvalidSkillName = errors.New("invalid skill name")

	// Conversation errors
	ErrInvalidChannelID = errors.New("invalid channel id")
)
