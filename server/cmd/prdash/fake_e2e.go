//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"

	"github.com/ATP-MiNTo/PRDashboards/server/internal/fake"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/netguard"
	"github.com/ATP-MiNTo/PRDashboards/server/internal/secrets"
)

func init() {
	extraFlags = func(fs *flag.FlagSet, o *options) {
		fs.BoolVar(&o.fake, "fake", false, "serve fake GitHub and Atlassian on loopback and keep secrets in memory (e2e build only)")
	}
	startFakes = func(_ context.Context, guard *netguard.Guard, log *slog.Logger) (*fakeEnv, error) {
		servers, err := fake.Start(guard, log)
		if err != nil {
			return nil, err
		}
		sec := secrets.NewMemory()
		for name, creds := range map[string]any{
			secrets.GitHubClient:    fake.GitHubClient,
			secrets.AtlassianClient: fake.AtlassianClient,
		} {
			raw, err := json.Marshal(creds)
			if err != nil {
				servers.Close()
				return nil, err
			}
			if err := sec.Set(name, raw); err != nil {
				servers.Close()
				return nil, err
			}
		}
		return &fakeEnv{Endpoints: servers.Endpoints, GitHubAPI: servers.GitHubAPI, Secrets: sec, Close: servers.Close}, nil
	}
}
