# s12ryt-ipv6

`s12ryt-ipv6` 是 Linux IPv6 代理節點管理器。它提供 SOCKS5、HTTP Proxy 與 mixed 入站，並把代理資料連線嚴格限制在 IPv6 socket；IPv4-only 目的地透過 DNS64/NAT64 存取。

管理介面是內嵌的繁體中文 SPA，預設以雙棧 wildcard 明文 HTTP 監聽 `:34466`。這會在網路上明文傳送管理密碼與操作資料，應只部署在已受信任或另有安全通道保護的網路。

## 平台與前置條件

- Debian 12/13 或 Ubuntu 24.04，`amd64` 或 `arm64`。
- root，或具備等效的 `CAP_NET_ADMIN` 權限。
- 核心與宿主機支援 IPv6、netlink 及 nftables。
- 上游 IPv6 前綴已路由到宿主機，且宿主機已有可用的 IPv6 default route。本程式不建立 default route。
- 若宿主機其他 nftables base chain 採 drop policy，必須自行允許管理與代理流量；本程式只管理 `inet s12ryt_ipv6` table，不修改其他規則。
- NAT64 是外部網路能力。公開 DNS64 resolver 只負責解析，不提供 NAT64 gateway。

## VPS 一鍵安裝與升級

安裝最新 GitHub Release：

```sh
curl -fsSL https://raw.githubusercontent.com/s12ryt/s12ryt-ipv6/main/install.sh | sudo sh
```

固定版本、自訂資料目錄與管理埠：

```sh
curl -fsSL https://raw.githubusercontent.com/s12ryt/s12ryt-ipv6/main/install.sh | sudo env VERSION=v1.2.3 DATA_DIR=/srv/s12ryt-ipv6 MANAGEMENT_PORT=45555 sh
```

安裝器僅支援 Debian 12/13、Ubuntu 24.04，以及 `amd64`、`arm64`。它會從 GitHub Release 下載檔案、核對 `checksums.txt`，再安裝並啟用 systemd 服務；重複執行即安全升級。若新版本未在 120 秒內回報 `healthy` 或 `degraded`，binary、systemd unit 與設定會回滾並重新驗證舊服務。

`VERSION` 預設為 `latest`；未設定 `MANAGEMENT_PORT` 時，升級會保留既有管理埠，首次安裝使用 `34466`。安裝器只會在 UFW 已安裝且已啟用時新增實際管理埠規則，絕不啟用 UFW，也不修改 IPv6 route。首次管理密碼只會從本次服務啟動後的 journal 擷取並顯示一次；若未取得，請執行下方的密碼重設命令。

管理介面仍是公網明文 HTTP。不要在未受信任網路直接暴露管理埠；應使用防火牆來源限制、VPN 或可信的反向代理安全通道。

## 從原始碼建置

需要 Go 1.25、Node.js 24 與 npm。

```sh
cd web
npm ci
npm test
npm run lint
npm run build
cd ..
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o s12ryt-ipv6 ./cmd/s12ryt-ipv6
```

建置 `arm64` 時把 `GOARCH` 改成 `arm64`。前端必須先建置，因為 Go binary 會 embed `web/dist`。

## 前景執行

```sh
sudo ./s12ryt-ipv6 serve --data-dir /etc/s12ryt-ipv6
```

首次啟動會把隨機管理員密碼只輸出一次到 stdout。資料目錄內的設定、狀態、master key、密碼雜湊、統計與日誌均分檔保存；目錄應維持 `0700`，秘密檔案維持 `0600`。

管理密碼可在服務運行或停止時安全重設：

```sh
sudo s12ryt-ipv6 admin reset-password --data-dir /etc/s12ryt-ipv6
```

## 離線 systemd 安裝

建置 binary 後，可用封裝內的離線安裝器；它與一鍵安裝器共用停止、健康檢查與回滾流程：

```sh
sudo sh deploy/install.sh ./s12ryt-ipv6
sudo systemctl status s12ryt-ipv6
sudo journalctl -u s12ryt-ipv6 -f
```

首次密碼可在第一次啟動的 journal 輸出中取得。確認登入後，應依組織的秘密管理流程處理該輸出。

移除服務與 binary，但保留狀態：

```sh
sudo sh deploy/uninstall.sh
```

## Docker Compose

Docker 只支援 Linux host。Compose 使用 host network，因代理需要直接綁定宿主機 IPv4/IPv6 位址與大量動態 port。

```sh
mkdir -p data
chmod 700 data
docker compose up --build -d
docker compose logs -f s12ryt-ipv6
```

`compose.yaml` 明確授予 `NET_ADMIN` 並把 `./data` 掛載到 `/etc/s12ryt-ipv6`。不要移除 host network、capability 或持久 volume；否則地址、route、nftables 或狀態保存無法正確運作。

## 網路配置流程

1. 在管理頁建立命名前綴範本，填入已路由的 `2000::/3` 內 CIDR、Linux 介面與配置模式。
2. 建立固定 IPv6、動態入站池或共享/專用出站池。
3. 建立節點，選擇 IPv4、IPv6 或雙棧入站，以及具名 IPv6 入站/出站資源。
4. 在「網路」頁確認 DoT、原生 IPv6、NAT64 與各出站資源的連通性。
5. 檢查宿主機其他防火牆規則；管理頁的 degraded 診斷不會自動修改外部 table/chain。

三種前綴模式：

- `address`：每個 IPv6 以 `/128` 配置到介面並等待 DAD。
- `local-route-freebind`：建立整段 local route，socket 使用 per-socket freebind。
- `external`：只驗證外部預配置，程式永不修改該地址或 route。

## 健康與日誌

- `GET /healthz` 未登入可讀，只回 `healthy`、`degraded` 或 `unhealthy`。
- JSONL 日誌預設為 `/etc/s12ryt-ipv6/events.jsonl`，100MB 輪替、保留五檔，並同步輸出 stdout/journal。
- 代理日誌不保存帳密、URL path、HTTP header 或內容。
- 正常停止會停止節點、移除程式擁有的 IPv6/local route 與 `inet s12ryt_ipv6` table，並保存統計。

## Linux integration 測試

真實 netlink/nftables 測試必須在一次性 network namespace 內以 root 執行：

```sh
sudo ip netns add s12ryt-test
sudo ip netns exec s12ryt-test env S12RYT_INTEGRATION_NETNS=1 go test -tags=integration ./internal/network ./internal/firewall -count=1
sudo ip netns del s12ryt-test
```

不要直接在生產宿主 namespace 執行 integration 測試。
