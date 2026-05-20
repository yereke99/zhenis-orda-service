package handler

import (
	"context"
	"fmt"
	"time"

	"zhenis-orda-service/internal/i18n"

	"go.uber.org/zap"
)

func (s *Server) StartSchedulers(ctx context.Context) {
	go s.every(ctx, time.Minute, s.runMinuteJobs)
	go s.every(ctx, 5*time.Second, s.runBroadcastJob)
	go s.every(ctx, 10*time.Minute, s.runInactiveReminderJob)
	go s.every(ctx, 24*time.Hour, s.runDailyJobs)
	go s.every(ctx, time.Minute, s.runLiveStreamReminderJob)
}

func (s *Server) runBroadcastJob(ctx context.Context) {
	if s.bot == nil {
		return
	}
	broadcast, err := s.store.ClaimQueuedBroadcast(ctx)
	if err != nil {
		s.logger.Warn("claim broadcast failed", zap.Error(err))
		return
	}
	if broadcast == nil {
		return
	}
	recipients, err := s.store.BroadcastRecipients(ctx, broadcast.Target)
	if err != nil {
		s.logger.Warn("broadcast recipients query failed", zap.String("broadcast_id", broadcast.ID), zap.Error(err))
		_ = s.store.FinishBroadcast(ctx, broadcast.ID, true)
		return
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	failed := false
	for _, recipient := range recipients {
		select {
		case <-ctx.Done():
			_ = s.store.FinishBroadcast(context.Background(), broadcast.ID, true)
			return
		case <-ticker.C:
		}
		if err := s.bot.SendMessage(ctx, recipient.TelegramID, broadcast.Body); err != nil {
			failed = true
			_ = s.store.RecordBroadcastMessage(ctx, broadcast.ID, recipient, "failed", err.Error())
			continue
		}
		_ = s.store.RecordBroadcastMessage(ctx, broadcast.ID, recipient, "sent", "")
	}
	if err := s.store.FinishBroadcast(ctx, broadcast.ID, failed); err != nil {
		s.logger.Warn("broadcast finish failed", zap.String("broadcast_id", broadcast.ID), zap.Error(err))
	}
}

func (s *Server) every(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	fn(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fn(ctx)
		}
	}
}

func (s *Server) runMinuteJobs(ctx context.Context) {
	if n, err := s.store.ExpirePendingPayments(ctx); err != nil {
		s.logger.Warn("expire pending payments failed", zap.Error(err))
	} else if n > 0 {
		s.logger.Info("expired pending payments", zap.Int64("count", n))
	}
	if n, err := s.store.ExpireSubscriptions(ctx); err != nil {
		s.logger.Warn("expire subscriptions failed", zap.Error(err))
	} else if n > 0 {
		s.logger.Info("expired subscriptions", zap.Int64("count", n))
	}
	if n, err := s.store.ExpireLevelTelegramInvites(ctx); err != nil {
		s.logger.Warn("expire level telegram invites failed", zap.Error(err))
	} else if n > 0 {
		s.logger.Info("expired level telegram invites", zap.Int64("count", n))
	}
	if n, err := s.store.ExpirePremiumCourseAccesses(ctx); err != nil {
		s.logger.Warn("expire premium course accesses failed", zap.Error(err))
	} else if n > 0 {
		s.logger.Info("expired premium course accesses", zap.Int64("count", n))
	}
	if n, err := s.store.ExpirePremiumCourseTelegramInvites(ctx); err != nil {
		s.logger.Warn("expire premium course telegram invites failed", zap.Error(err))
	} else if n > 0 {
		s.logger.Info("expired premium course telegram invites", zap.Int64("count", n))
	}
}

func (s *Server) runInactiveReminderJob(ctx context.Context) {
	if s.bot == nil {
		return
	}
	users, err := s.store.ListInactiveUsers(ctx, time.Now().Add(-72*time.Hour), 200)
	if err != nil {
		s.logger.Warn("inactive users query failed", zap.Error(err))
		return
	}
	for _, user := range users {
		key := fmt.Sprintf("reminder:inactive3:%s", user.ID)
		ok, err := s.kv.SetNX(ctx, key, "1", s.cfg.InactiveReminderCooldown)
		if err != nil || !ok {
			continue
		}
		_ = s.bot.SendMessage(ctx, user.TelegramID, i18n.T(user.Language, "inactive_3_days"))
	}
}

func (s *Server) runDailyJobs(ctx context.Context) {
	if s.bot == nil {
		return
	}
	from := time.Now().Add(time.Duration(s.cfg.SubscriptionReminderHours-1) * time.Hour)
	to := time.Now().Add(time.Duration(s.cfg.SubscriptionReminderHours+1) * time.Hour)
	subs, err := s.store.ListSubscriptionsExpiringBetween(ctx, from, to)
	if err != nil {
		s.logger.Warn("subscription reminder query failed", zap.Error(err))
		return
	}
	for _, sub := range subs {
		key := fmt.Sprintf("reminder:sub3:%s:%s", sub.ID, sub.ExpiresAt.Format("2006-01-02"))
		ok, err := s.kv.SetNX(ctx, key, "1", 7*24*time.Hour)
		if err != nil || !ok {
			continue
		}
		user, err := s.store.GetUserByID(ctx, sub.UserID)
		if err == nil {
			_ = s.bot.SendMessage(ctx, user.TelegramID, i18n.T(user.Language, "subscription_ending"))
		}
	}
}

func (s *Server) runLiveStreamReminderJob(ctx context.Context) {
	if s.bot == nil {
		return
	}
	streams, err := s.store.ListStreams(ctx, "", true)
	if err != nil {
		return
	}
	now := time.Now()
	windows := []struct {
		key string
		dur time.Duration
	}{
		{"24h", 24 * time.Hour},
		{"3h", 3 * time.Hour},
		{"30m", 30 * time.Minute},
	}
	for _, stream := range streams {
		if stream.Status != "scheduled" {
			continue
		}
		until := time.Until(stream.StartsAt)
		for _, window := range windows {
			if until <= window.dur && until > window.dur-time.Minute {
				key := fmt.Sprintf("reminder:stream:%s:%s", stream.ID, window.key)
				ok, err := s.kv.SetNX(ctx, key, "1", 48*time.Hour)
				if err != nil || !ok {
					continue
				}
				users, err := s.store.ListUsersWithActiveTariffAtLeast(ctx, stream.TariffRequirement, 1000)
				if err != nil {
					s.logger.Warn("live stream users query failed", zap.Error(err))
					continue
				}
				for _, user := range users {
					text := fmt.Sprintf("ZHABYQ RAZBOR NIGHT: %s\n%s қалды.\nЭфирді өткізіп алмаңыз.", stream.Title, window.key)
					_ = s.bot.SendMessage(ctx, user.TelegramID, text)
				}
				s.logger.Info("live stream reminders sent", zap.String("stream_id", stream.ID), zap.String("window", window.key), zap.Int("users", len(users)), zap.Time("now", now))
			}
		}
	}
}
