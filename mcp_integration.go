package main

import (
	"github.com/kw-aficionado/familiehub/internal/mcpaccess"
	"log"
)

func (a *App) mcpRoutes() {
	_, err := mcpaccess.Register(mcpaccess.Config{
		DB: a.db, Mux: a.mux, Admin: a.withAdmin, Notify: a.broadcast,
		BaseURL: a.cfg.AppURL, Location: a.loc,
	})
	if err != nil {
		log.Printf("MCP integration unavailable: %v", err)
	}
}
