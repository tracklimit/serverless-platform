// Package platform holds cross-cutting assets that need to live at the
// module root. It exists because //go:embed directives cannot reference
// parent directories, so any code that embeds migrations/ must be
// declared at or above the migrations/ path.
package platform

import "embed"

// MigrationsFS is the embedded filesystem containing all SQL migration
// files. It is consumed by internal/db.RunMigrations.
//
//go:embed all:migrations
var MigrationsFS embed.FS
