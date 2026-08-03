module github.com/s12ryt/s12ryt-ipv6

go 1.25.0

require (
	github.com/google/nftables v0.3.0
	github.com/miekg/dns v1.1.72
	github.com/vishvananda/netlink v1.3.1
	golang.org/x/crypto v0.54.0
	golang.org/x/sys v0.47.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/mdlayher/netlink v1.7.3-0.20250113171957-fbb4dce95f42 // indirect
	github.com/mdlayher/socket v0.5.0 // indirect
	github.com/s12ryt/s12ryt-ipv6/webui v0.0.0
	github.com/things-go/go-socks5 v0.1.1
	github.com/vishvananda/netns v0.0.5 // indirect
	golang.org/x/mod v0.31.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/tools v0.40.0 // indirect
)

replace github.com/s12ryt/s12ryt-ipv6/webui => ./web
