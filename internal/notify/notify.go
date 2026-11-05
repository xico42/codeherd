// Package notify sends desktop notifications.
package notify

import (
	"fmt"

	"github.com/gen2brain/beeep"
)

// Notifier abstracts desktop notification sending for testability.
type Notifier interface {
	Notify(title, message, appIcon string) error
}

// beeepNotifier wraps beeep.Notify.
type beeepNotifier struct{}

func (b beeepNotifier) Notify(title, message, appIcon string) error {
	if err := beeep.Notify(title, message, appIcon); err != nil {
		return fmt.Errorf("beeep notify: %w", err)
	}
	return nil
}

// Service sends desktop notifications.
type Service struct {
	notifier Notifier
}

// NewService creates a Service with the given notifier.
func NewService(n Notifier) *Service {
	return &Service{notifier: n}
}

// NewDefaultService creates a Service using beeep for real notifications.
func NewDefaultService() *Service {
	return &Service{notifier: beeepNotifier{}}
}

// Send dispatches a desktop notification.
func (s *Service) Send(title, message string) error {
	if err := s.notifier.Notify(title, message, ""); err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	return nil
}
