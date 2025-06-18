module github.com/HyPE-Network/vanilla-proxy

go 1.24.0

require (
	github.com/getsentry/sentry-go v0.31.1
	github.com/go-gl/mathgl v1.2.0
	github.com/gofrs/flock v0.12.1
	github.com/google/uuid v1.6.0
	github.com/pelletier/go-toml v1.9.5
	github.com/sandertv/go-raknet v1.14.3-0.20250305181847-6af3e95113d6
	github.com/sandertv/gophertunnel v1.45.0
	github.com/tailscale/hujson v0.0.0-20250226034555-ec1d1c113d33
)

require (
	github.com/go-jose/go-jose/v4 v4.0.5 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/muhammadmuzzammil1998/jsonc v1.0.0 // indirect
	golang.org/x/crypto v0.36.0 // indirect
	golang.org/x/net v0.37.0 // indirect
	golang.org/x/oauth2 v0.28.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	golang.org/x/text v0.23.0 // indirect
)

replace github.com/sandertv/gophertunnel => github.com/smell-of-curry/gophertunnel v1.46.1-0.20250618215601-3e46e37f746e

replace github.com/sandertv/go-raknet => github.com/smell-of-curry/go-raknet v0.0.0-20250525005230-991ee492a907
