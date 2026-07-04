package firebase

import (
	"context"
	"fmt"

	firebaseAdmin "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"

	"github.com/isyll/go-grpc-starter/pkg/config"
)

type Client struct {
	app *firebaseAdmin.App
}

func NewClient(cfg *config.FirebaseConfig) (*Client, error) {
	opt := option.WithAuthCredentialsFile(option.ServiceAccount, cfg.CredentialsFile)

	app, err := firebaseAdmin.NewApp(context.Background(), &firebaseAdmin.Config{
		ProjectID: cfg.ProjectID,
	}, opt)
	if err != nil {
		return nil, fmt.Errorf("initialize firebase app: %w", err)
	}

	return &Client{app: app}, nil
}

// GetMessagingClient returns the FCM messaging client.
func (c *Client) GetMessagingClient(ctx context.Context) (*messaging.Client, error) {
	client, err := c.app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("get messaging client: %w", err)
	}
	return client, nil
}
