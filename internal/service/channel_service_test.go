package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/service"
)

func TestChannelService_SyncChannels(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("fails when client not connected", func(t *testing.T) {
		t.Parallel()
		channelRepo := newMockChannelRepo()
		client := &mockTelegramClient{connected: false}
		rebuilder := &mockSnapshotRebuilder{}
		svc := service.NewChannelService(channelRepo, client, rebuilder)

		err := svc.SyncChannels(ctx)
		if !errors.Is(err, domain.ErrTelegramNotConfigured) {
			t.Errorf("expected ErrTelegramNotConfigured, got %v", err)
		}
	})

	t.Run("syncs dialogs and preserves existing flags", func(t *testing.T) {
		t.Parallel()
		channelRepo := newMockChannelRepo()
		// Existing channel with custom flags
		_ = channelRepo.UpsertChannel(ctx, &domain.TelegramChannel{
			ChatID:          1001,
			Title:           "Existing Group",
			MonitorProducts: true,
			MonitorCoupons:  true,
		})

		client := &mockTelegramClient{
			connected: true,
			dialogs: []*domain.TelegramChannel{
				{ChatID: 1001, Title: "Existing Group Updated Name"},
				{ChatID: 1002, Title: "Brand New Group"},
			},
		}
		rebuilder := &mockSnapshotRebuilder{}
		svc := service.NewChannelService(channelRepo, client, rebuilder)

		if err := svc.SyncChannels(ctx); err != nil {
			t.Fatalf("sync channels failed: %v", err)
		}

		ch1001, err := channelRepo.GetChannel(ctx, 1001)
		if err != nil {
			t.Fatalf("get 1001 failed: %v", err)
		}
		// Preserved flags
		if !ch1001.MonitorProducts || !ch1001.MonitorCoupons {
			t.Errorf("expected flags preserved for 1001")
		}
		if ch1001.Title != "Existing Group Updated Name" {
			t.Errorf("expected updated title, got %s", ch1001.Title)
		}

		ch1002, err := channelRepo.GetChannel(ctx, 1002)
		if err != nil {
			t.Fatalf("get 1002 failed: %v", err)
		}
		if ch1002.MonitorProducts || ch1002.MonitorCoupons {
			t.Errorf("expected new channel default flags to be false")
		}

		rebuilder.mu.Lock()
		calls := rebuilder.calls
		rebuilder.mu.Unlock()
		if calls != 1 {
			t.Errorf("expected 1 rebuild snapshot call, got %d", calls)
		}
	})
}

func TestChannelService_UpdateFlags(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	channelRepo := newMockChannelRepo()
	_ = channelRepo.UpsertChannel(ctx, &domain.TelegramChannel{
		ChatID:          2001,
		Title:           "Promo Group",
		MonitorProducts: false,
		MonitorCoupons:  false,
	})

	client := &mockTelegramClient{connected: true}
	rebuilder := &mockSnapshotRebuilder{}
	svc := service.NewChannelService(channelRepo, client, rebuilder)

	if err := svc.UpdateChannelFlags(ctx, 2001, true, true); err != nil {
		t.Fatalf("update flags failed: %v", err)
	}

	ch, err := channelRepo.GetChannel(ctx, 2001)
	if err != nil {
		t.Fatalf("get channel failed: %v", err)
	}
	if !ch.MonitorProducts || !ch.MonitorCoupons {
		t.Errorf("expected flags to be true")
	}

	channels, err := svc.ListChannels(ctx)
	if err != nil {
		t.Fatalf("list channels failed: %v", err)
	}
	if len(channels) != 1 {
		t.Errorf("expected 1 channel, got %d", len(channels))
	}

	rebuilder.mu.Lock()
	calls := rebuilder.calls
	rebuilder.mu.Unlock()
	if calls != 1 {
		t.Errorf("expected 1 rebuild snapshot call, got %d", calls)
	}
}
