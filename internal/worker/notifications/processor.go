package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"runtime/debug"
	"strings"
	"time"

	"firebase.google.com/go/v4/messaging"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"

	"github.com/isyll/go-grpc-starter/internal/metrics"
	"github.com/isyll/go-grpc-starter/internal/models"
	"github.com/isyll/go-grpc-starter/pkg/config"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

type Processor struct {
	fcmClient       *messaging.Client
	fcmTokenRepo    FCMTokenRepository
	preferencesRepo NotificationPreferencesRepository
	templateRepo    NotificationTemplateRepository
	logRepo         NotificationLogRepository
	cfg             *config.NotificationsConfig
	logger          *logger.Logger
}

func NewProcessor(
	fcmClient *messaging.Client,
	fcmTokenRepo FCMTokenRepository,
	preferencesRepo NotificationPreferencesRepository,
	templateRepo NotificationTemplateRepository,
	logRepo NotificationLogRepository,
	cfg *config.NotificationsConfig,
	logx *logger.Logger,
) *Processor {
	return &Processor{
		fcmClient:       fcmClient,
		fcmTokenRepo:    fcmTokenRepo,
		preferencesRepo: preferencesRepo,
		templateRepo:    templateRepo,
		logRepo:         logRepo,
		cfg:             cfg,
		logger:          logx,
	}
}

func (p *Processor) ProcessTask(
	ctx context.Context,
	t *asynq.Task,
) (err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			p.logger.Error(
				"notifications worker panic recovered",
				"task_type", t.Type(),
				"panic", r,
				"stack_trace", string(stack),
			)
			metrics.WorkerPanicsTotal.
				WithLabelValues("notifications").
				Inc()
			err = fmt.Errorf(
				"notifications worker panic: %v", r,
			)
		}
	}()

	var event Event
	if err := json.Unmarshal(t.Payload(), &event); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	return p.processNotification(ctx, &event)
}

func (p *Processor) processNotification(
	ctx context.Context,
	event *Event,
) error {
	if !p.shouldSendNotification(ctx, event) {
		p.logger.Debug("Notification skipped due to preferences",
			"event_type", event.Type,
			"user_id", event.UserID,
		)
		return nil
	}

	tokens, err := p.fcmTokenRepo.FindActiveByUserID(ctx, event.UserID)
	if err != nil {
		return fmt.Errorf("failed to get FCM tokens: %w", err)
	}

	if len(tokens) == 0 {
		p.logger.Debug("No active FCM tokens for user",
			"user_id", event.UserID,
			"event_type", event.Type,
		)
		return nil
	}

	template, err := p.templateRepo.FindByEventType(ctx, event.Type)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			p.logger.Warn("No template found for event type",
				"event_type", event.Type,
			)
			return nil
		}
		return fmt.Errorf("failed to get template: %w", err)
	}

	var lastErr error
	for _, token := range tokens {
		result := p.sendToToken(ctx, event, template, token)
		if err := p.logNotification(ctx, event, token, result); err != nil {
			p.logger.Error("Failed to log notification",
				"error", err,
				"event_type", event.Type,
			)
		}
		if !result.Success {
			lastErr = errors.New(result.ErrorMessage)
		}
	}

	return lastErr
}

func (p *Processor) shouldSendNotification(
	ctx context.Context,
	event *Event,
) bool {
	prefs, err := p.preferencesRepo.FindByUserID(ctx, event.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			p.logger.Debug(
				"No preferences found for user, sending notification",
				"user_id",
				event.UserID,
			)
			return true
		}
		p.logger.Error(
			"Failed to get preferences",
			"error",
			err,
			"user_id",
			event.UserID,
		)
		return true
	}

	category := EventCategory(event.Type)
	if !p.isCategoryEnabled(prefs, category) {
		return false
	}

	if p.cfg.QuietHours.CheckEnabled && prefs.QuietHoursEnabled {
		if p.isQuietHours(prefs) {
			return false
		}
	}

	return true
}

func (p *Processor) isCategoryEnabled(
	prefs *models.NotificationPreferences,
	category string,
) bool {
	switch category {
	case "marketing":
		return prefs.Marketing
	default:
		return prefs.Push
	}
}

func (p *Processor) isQuietHours(
	prefs *models.NotificationPreferences,
) bool {
	if prefs.QuietHoursStart == nil || prefs.QuietHoursEnd == nil {
		return false
	}

	tz := prefs.Timezone
	if tz == "" {
		tz = p.cfg.QuietHours.DefaultTimezone
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		p.logger.Warn("Invalid timezone, using UTC", "timezone", tz)
		loc = time.UTC
	}

	now := time.Now().In(loc)
	currentTime := now.Format("15:04:05")

	start := *prefs.QuietHoursStart
	end := *prefs.QuietHoursEnd

	if start > end {
		return currentTime >= start || currentTime < end
	}
	return currentTime >= start && currentTime < end
}

func (p *Processor) sendToToken(
	ctx context.Context,
	event *Event,
	template *models.NotificationTemplate,
	token *models.FCMToken,
) *SendResult {
	msg := p.buildFCMMessage(event, template, token)

	messageID, err := p.fcmClient.Send(ctx, msg)
	if err != nil {
		p.logger.Error("FCM send failed",
			"error", err,
			"token_id", token.ID,
			"event_type", event.Type,
		)

		if messaging.IsUnregistered(err) ||
			messaging.IsInvalidArgument(err) {
			if deactErr := p.fcmTokenRepo.
				DeactivateByID(ctx, token.ID); deactErr != nil {
				p.logger.Error(
					"Failed to deactivate token",
					"error",
					deactErr,
				)
			}
		}

		return &SendResult{
			Success:      false,
			FCMTokenID:   token.ID,
			ErrorCode:    "FCM_ERROR",
			ErrorMessage: err.Error(),
		}
	}

	if err := p.fcmTokenRepo.UpdateLastUsed(ctx, token.ID); err != nil {
		p.logger.Warn("Failed to update last_used_at", "error", err)
	}

	return &SendResult{
		Success:    true,
		FCMTokenID: token.ID,
		MessageID:  messageID,
	}
}

func (p *Processor) buildFCMMessage(
	event *Event,
	template *models.NotificationTemplate,
	token *models.FCMToken,
) *messaging.Message {
	var title, body string
	lang := "en"
	for _, t := range template.Translations {
		if t.Language == lang {
			title = p.interpolate(t.Title, event.Data)
			body = p.interpolate(t.Body, event.Data)
			break
		}
	}

	deepLink := buildDeepLink(p.cfg, event.Type, event.Data)

	data := map[string]string{
		"event_type":   event.Type,
		"click_action": "FLUTTER_NOTIFICATION_CLICK",
	}
	maps.Copy(data, event.Data)

	if deepLink != "" {
		data["deep_link"] = deepLink
	}
	if template.Action != nil {
		data["action"] = *template.Action
	}

	msg := &messaging.Message{
		Token: token.Token,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
	}

	priority := template.Priority
	if priority == "" {
		priority = string(event.Priority)
	}

	msg.Android = &messaging.AndroidConfig{
		Priority: priority,
		Notification: &messaging.AndroidNotification{
			Sound:     template.Sound,
			ChannelID: p.getChannelID(template),
		},
	}

	msg.APNS = &messaging.APNSConfig{
		Headers: map[string]string{
			"apns-priority": p.getAPNSPriority(priority),
		},
		Payload: &messaging.APNSPayload{
			Aps: &messaging.Aps{
				Sound: template.Sound,
			},
		},
	}

	return msg
}

func (p *Processor) interpolate(
	template string,
	data map[string]string,
) string {
	result := template
	for key, value := range data {
		placeholder := "{" + key + "}"
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

func (p *Processor) getChannelID(
	template *models.NotificationTemplate,
) string {
	if template.AndroidChannelID != nil {
		return *template.AndroidChannelID
	}
	return "default"
}

func (p *Processor) getAPNSPriority(priority string) string {
	if priority == "high" {
		return "10"
	}
	return "5"
}

func (p *Processor) logNotification(
	ctx context.Context,
	event *Event,
	token *models.FCMToken,
	result *SendResult,
) error {
	tokenID := token.ID

	status := models.NotificationStatusSent
	var errCode, errMsg *string
	if !result.Success {
		status = models.NotificationStatusFailed
		errCode = &result.ErrorCode
		errMsg = &result.ErrorMessage
	}

	payload := models.JSONB{
		"data":     event.Data,
		"priority": event.Priority,
	}

	log := &models.NotificationLog{
		UserID:       &event.UserID,
		EventType:    event.Type,
		EventID:      &result.MessageID,
		FCMTokenID:   &tokenID,
		Status:       status,
		ErrorCode:    errCode,
		ErrorMessage: errMsg,
		Payload:      &payload,
		SentAt:       time.Now(),
	}

	return p.logRepo.Create(ctx, log)
}
