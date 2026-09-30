// Package i18n embeds the interface translations, which notifications reuse.
package i18n

import "embed"

//go:embed ru.json en.json
var Files embed.FS
