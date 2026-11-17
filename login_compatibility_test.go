package main

import (
 "strings"
 "testing"
)

// APP_PASSWORD historically permits long passphrases. Account-password limits
// must not silently lock out an existing administrator after an upgrade.
func TestLoginPreservesLongAdministratorPassphrases(t *testing.T) {
 for _, username := range []string{"", " ADMIN "} {
  t.Run(username, func(t *testing.T) {
   a, _ := testApp(t)
   a.cfg.Password = strings.Repeat("fictional-long-passphrase-", 12)
   tabletExpect(t, tabletCall(a, "POST", "/api/login", map[string]string{"username":username,"password":a.cfg.Password+"wrong"}, "", "", nil), 401)
   tabletExpect(t, tabletCall(a, "POST", "/api/login", map[string]string{"username":username,"password":a.cfg.Password}, "", "", nil), 200)
   tabletExpect(t, tabletCall(a, "POST", "/api/login", map[string]string{"username":"ordinary-account","password":a.cfg.Password}, "", "", nil), 400)
  })
 }
}
