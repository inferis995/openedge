package models

import "time"

// WriteCommand is the body of every cmd/write/{gateway} message: the platform
// asking a driver to write one value to one tag.
//
// It was declared five times — once in each of four drivers and once more in
// each publisher — and a field added to one copy reaches no other. IssuedAt is
// such a field: a publisher that does not set it produces commands that never
// expire, and a driver that does not read it executes them however old they
// are. See internal/commands for why that matters.
type WriteCommand struct {
	TagID    int         `json:"tag_id"`
	Code     string      `json:"code"`
	Value    interface{} `json:"value"`
	DataType string      `json:"data_type"`

	// IssuedAt is when the command was given, in Unix milliseconds. Zero only
	// from a publisher older than the field.
	IssuedAt int64 `json:"issued_at,omitempty"`

	// RecipeRunID ties the write to a recipe load, so its result can be
	// recorded against the run.
	RecipeRunID int64 `json:"recipe_run_id,omitempty"`
}

// NewWriteCommand builds a command stamped with the current time. Publishers
// use it rather than a literal so the stamp cannot be forgotten.
func NewWriteCommand(tagID int, code string, value interface{}, dataType string) WriteCommand {
	return WriteCommand{
		TagID:    tagID,
		Code:     code,
		Value:    value,
		DataType: dataType,
		IssuedAt: time.Now().UnixMilli(),
	}
}
