package service

import (
	"context"
	"fmt"

	"github.com/matrocheetos/telegram-monitor/internal/domain"
	"github.com/matrocheetos/telegram-monitor/internal/port"
)

var _ port.ChannelUseCase = (*ChannelService)(nil)

type ChannelService struct {
	channelRepo       port.ChannelRepository
	telegramClient    port.TelegramClientPort
	snapshotRebuilder port.SnapshotRebuilder
}

func NewChannelService(
	channelRepo port.ChannelRepository,
	telegramClient port.TelegramClientPort,
	snapshotRebuilder port.SnapshotRebuilder,
) *ChannelService {
	return &ChannelService{
		channelRepo:       channelRepo,
		telegramClient:    telegramClient,
		snapshotRebuilder: snapshotRebuilder,
	}
}

func (s *ChannelService) SyncChannels(ctx context.Context) error {
	if !s.telegramClient.IsConnected() {
		return domain.ErrTelegramNotConfigured
	}

	channels, err := s.telegramClient.SyncDialogs(ctx)
	if err != nil {
		return fmt.Errorf("sync dialogs: %w", err)
	}

	for _, ch := range channels {
		existing, err := s.channelRepo.GetChannel(ctx, ch.ChatID)
		if err == nil && existing != nil {
			ch.MonitorProducts = existing.MonitorProducts
			ch.MonitorCoupons = existing.MonitorCoupons
		}

		if err := s.channelRepo.UpsertChannel(ctx, ch); err != nil {
			return fmt.Errorf("upsert channel %d: %w", ch.ChatID, err)
		}
	}

	if s.snapshotRebuilder != nil {
		if err := s.snapshotRebuilder.RebuildSnapshot(ctx); err != nil {
			return fmt.Errorf("rebuild snapshot: %w", err)
		}
	}

	return nil
}

func (s *ChannelService) ListChannels(ctx context.Context) ([]*domain.TelegramChannel, error) {
	return s.channelRepo.ListChannels(ctx)
}

func (s *ChannelService) UpdateChannelFlags(ctx context.Context, chatID int64, monitorProducts, monitorCoupons bool) error {
	if err := s.channelRepo.UpdateChannelFlags(ctx, chatID, monitorProducts, monitorCoupons); err != nil {
		return fmt.Errorf("update channel flags: %w", err)
	}

	if s.snapshotRebuilder != nil {
		if err := s.snapshotRebuilder.RebuildSnapshot(ctx); err != nil {
			return fmt.Errorf("rebuild snapshot: %w", err)
		}
	}

	return nil
}
