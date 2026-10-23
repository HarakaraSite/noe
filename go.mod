module forge.harakara.site/littleisland/noe/v2

go 1.27.2

require (
	forge.harakara.site/littleisland/noe/third_party/go-quickjs v0.28.1-noe.1
	github.com/OpenRouterTeam/go-sdk v0.9.61
	github.com/openai/openai-go/v3 v3.79.0
	golang.org/x/image v0.47.0
	golang.org/x/sys v0.49.0
)

replace forge.harakara.site/littleisland/noe/third_party/go-quickjs => ./third_party/go-quickjs

require (
	github.com/coder/websocket v1.8.15 // indirect
	github.com/go-quickjs/go-intl v0.6.1 // indirect
	github.com/spyzhov/ajson v0.9.6 // indirect
	github.com/tidwall/gjson v1.20.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.2 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
)
