package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/edgesync"
)

// configSyncInterval is how often the box asks the central platform what it is
// supposed to be doing.
//
// A minute, because a change made in the web interface should reach the plant
// while the person who made it is still looking at the screen. The request is a
// few kilobytes; on a metered 4G link that is a couple of megabytes a month.
const configSyncInterval = time.Minute

// runConfigSync keeps the box's own database a mirror of the central one.
//
// The box has no path to the central database — that is why it exists — so the
// configuration comes down over HTTP and is written locally. Everything after
// that reads only the local copy: the drivers, the alarm manager and the
// container supervisor never know whether the link is up.
//
// That is the property worth protecting. A plant does not stop being monitored
// because a 4G modem is sulking; it stops being RECONFIGURED, which is a very
// different thing and one nobody notices at three in the morning.
func runConfigSync(ctx context.Context, database *sql.DB, baseURL, apiKey string) {
	if baseURL == "" || apiKey == "" {
		log.Printf("[CONFIG-SYNC] no central platform configured (CORE_API_URL / CORE_API_TOKEN): " +
			"this box will run on whatever configuration is already in its database")
		return
	}

	log.Printf("[CONFIG-SYNC] mirroring the configuration from %s every %s", baseURL, configSyncInterval)

	// Once immediately: a box that has just been switched on has an empty
	// database and nothing to poll until this lands.
	syncOnce(ctx, database, baseURL, apiKey)

	ticker := time.NewTicker(configSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncOnce(ctx, database, baseURL, apiKey)
		}
	}
}

// lastSyncFailed suppresses the repetition of an unreachable-platform message.
// A box on a link that is down for a week would otherwise write ten thousand
// identical lines over the one that says what actually happened.
var lastSyncFailed bool

func syncOnce(ctx context.Context, database *sql.DB, baseURL, apiKey string) {
	cfg, err := edgesync.Fetch(ctx, baseURL, apiKey)
	if err != nil {
		if !lastSyncFailed {
			log.Printf("[CONFIG-SYNC] cannot reach the central platform, carrying on with the "+
				"configuration already stored: %v", err)
			lastSyncFailed = true
		}
		return
	}
	if lastSyncFailed {
		log.Printf("[CONFIG-SYNC] the central platform is reachable again")
		lastSyncFailed = false
	}

	if err := edgesync.Apply(ctx, database, cfg); err != nil {
		// Apply is one transaction, so a failure leaves the previous
		// configuration intact rather than half of the new one.
		log.Printf("[CONFIG-SYNC] the configuration could not be applied, the previous one is "+
			"still in force: %v", err)
		return
	}

	sites, areas, gateways, tags, alarms := cfg.Counts()
	log.Printf("[CONFIG-SYNC] configuration for %q: %d sites, %d areas, %d gateways, %d tags, %d alarm rules",
		cfg.OrgName, sites, areas, gateways, tags, alarms)
}
