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

安裝器僅支援 Debian 12/13、Ubuntu 24.04，以及 `amd64`、`arm64`。它會從 GitHub Release 下載檔案、核對 `checksums.txt`，再安裝並啟用 systemd 服務；重複執行即安全升級。新版本必須在 120 秒內通過 HTTP health 與本機 Agent control socket 兩道檢查；任一道失敗時，binary、systemd unit 與設定會回滾並重新驗證舊服務。`healthy` 與結構有效的 `degraded` 都視為服務已啟動，安裝成功後會列出不含秘密的 Agent CLI quickstart。

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

## 本機 Agent CLI

`s12ryt-ipv6 agent ...` 是供本機 Agent、自動化腳本及管理員使用的機器介面。它直接連到資料目錄內權限為 `0600` 的 `control.sock`，不經明文 HTTP 管理介面、session 或 CSRF；預設資料目錄下必須以 root 或 `sudo` 執行。一般成功與所有失敗都只在 stdout 輸出單一 JSON，`schema` 與 `export` 成功時則直接輸出文件。

先確認服務與可用 schema：

```sh
sudo s12ryt-ipv6 agent status --data-dir /etc/s12ryt-ipv6
sudo s12ryt-ipv6 agent schema --data-dir /etc/s12ryt-ipv6 > agent-schema.json
```

匯出預設會以 `authentication.action: preserve` 遮罩節點帳密，因此文件可安全 round-trip，不會旋轉既有帳密。只有明示 `--show-secrets` 才輸出明文帳密；應避免將該輸出寫入日誌、版本庫或不受保護的暫存檔。

```sh
sudo s12ryt-ipv6 agent export --format json --data-dir /etc/s12ryt-ipv6 > agent-config.json
sudo s12ryt-ipv6 agent export --format yaml --data-dir /etc/s12ryt-ipv6 > agent-config.yaml
sudo s12ryt-ipv6 agent export --format yaml --data-dir /etc/s12ryt-ipv6 \
  | sudo s12ryt-ipv6 agent apply --format yaml --dry-run --data-dir /etc/s12ryt-ipv6
```

套用文件時必須明示 JSON 或 YAML 格式。`--dry-run` 會完成整份文件、資源依賴與節點引用預檢但不修改狀態；預設保留文件未列出的物件。只有 `--prune --yes` 會刪除文件中明示區段的未列物件，省略的整個區段不受影響。

```sh
sudo s12ryt-ipv6 agent apply --format yaml --file ./agent-config.yaml --dry-run --data-dir /etc/s12ryt-ipv6
sudo s12ryt-ipv6 agent apply --format yaml --file ./agent-config.yaml --data-dir /etc/s12ryt-ipv6
sudo s12ryt-ipv6 agent apply --format yaml --file ./agent-config.yaml --prune --yes --data-dir /etc/s12ryt-ipv6
```

命令範圍：

| 範圍 | 命令 |
| --- | --- |
| 狀態與宣告 | `status`、`schema`、`export`、`apply` |
| IPv6 資源 | `resources list`；`template create/delete`；`fixed create/delete`；`pool create/delete/refresh/force-drain` |
| 節點 | `nodes list/get/create/update/delete/start/stop/batch-create/move` |
| 資料夾 | `folders rename/start/stop/delete` |
| 網路 | `network show/test`；`nat64 set/clear`；`resolvers replace` |
| 觀察與維護 | `logs tail/clear`；`stats show/reset` |

複合的 create/update/batch/resolver 輸入使用 JSON `--file PATH`，`--file -` 或省略時讀 stdin；簡單 selector 使用 `--id`、`--name`、`--folder` 等旗標。所有 delete、`force-drain`、`logs clear`、`stats reset` 與 apply `--prune` 都要求 `--yes`，CLI 不會顯示互動提示。單步命令預設逾時 30 秒，apply 預設 10 分鐘，可用 `--timeout` 覆寫為 1 秒至 30 分鐘。

全域設定採欄位級合併。管理埠、代理埠範圍、最大節點數與 production 啟動期 dial timeout 等變更會安全保存並在回應列為 `restart_required`，CLI 不會自行呼叫 systemd；重新啟動後 `status` 的 active/configured 值才會一致。`degraded` 或 `unhealthy` 的 `status` 仍輸出有效 JSON，但退出碼為 `1`。

## 離線 systemd 安裝

建置 binary 後，可用封裝內的離線安裝器；它與一鍵安裝器共用停止、HTTP/Agent 健康檢查與回滾流程：

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

## 管理面板模式

登入後可在頂部工具列切換「基礎」與「進階」模式。模式只保存在目前瀏覽器的 `localStorage`，不會寫入伺服器，也不會改變代理節點的執行方式。第一次使用預設為基礎模式。

- **基礎模式**：保留五個管理頁與日常操作，只隱藏較少使用或容易誤設的欄位。適合第一次部署、一般節點管理與日常監看。
- **進階模式**：顯示目前支援的完整設定，適合需要調整連線限制、逾時、ULA、DNS64/NAT64、DoT resolver、池容量或釘選地址的管理員。
- 切換模式不會關閉已展開的表單，也不會清除尚未送出的輸入。
- 編輯既有節點時，基礎模式隱藏的進階值會原樣保留；建立新節點時則使用經測試的安全預設值。

各頁在兩種模式下的操作差異：

| 頁面 | 基礎模式 | 進階模式 | 建議使用時機 |
| --- | --- | --- | --- |
| 總覽 | 健康狀態、NAT64、節點與流量摘要 | 與基礎模式相同 | 登入後先確認整體服務是否正常 |
| 節點 | 建立、編輯、啟停、帳密、入站／出站資源、代理埠、刪除 | 另可調 TCP/UDP 上限、四種逾時與 ULA 政策 | 建立代理入口，或變更認證與使用的 IPv6 資源 |
| IPv6 資源 | 簡化建立前綴、固定地址與池；保留刷新、刪除及強制排空 | 另可選配置模式、手填固定地址、容量與釘選地址 | 先建立可供節點選取的具名 IPv6 資源 |
| 網路 | NAT64／防火牆診斷、連通性測試、管理密碼 | 另可覆寫 NAT64 `/96` 及編輯 DoT resolver | 排查解析、IPv6 路徑、NAT64 或外部防火牆問題 |
| 日誌 | 全部查詢條件、事件與統計檢視 | 另可清除全部日誌及歸零單節點／全部統計 | 追查連線失敗、來源、目的與選用的出站地址 |

基礎模式建立資源時使用以下預設：前綴採 `address` 模式、固定地址自動生成；動態入站池容量為 10、共享出站池為 100、專用出站池為 15，且不釘選既有固定地址。需要其他配置時再切換進階模式。

## 詞彙與功能說明

### 代理與資源

| 名詞 | 說明 |
| --- | --- |
| 節點 | 一個可獨立啟停的代理入口，包含協定、認證、監聽埠、入站資源、出站資源、限制與逾時設定。 |
| SOCKS5 | 支援 TCP `CONNECT` 與 UDP `ASSOCIATE` 的代理協定；不支援 `BIND`。 |
| HTTP Proxy | 支援一般 absolute-form HTTP 轉送與 HTTPS 常用的 `CONNECT` tunnel。 |
| mixed | 在同一個監聽位址與埠辨識 SOCKS5 或 HTTP Proxy。 |
| 入站 | 用戶端連入代理的位址與埠。節點可使用 IPv4 wildcard、具名固定 IPv6、具名動態入站池，或雙棧組合。 |
| 出站 | 代理連向目的地時綁定的來源 IPv6。節點必須選擇具名固定地址、共享出站池或專用出站池。 |
| 前綴範本 | 一組具名的 IPv6 CIDR、Linux 介面與配置模式。固定地址與各種池都從範本取得地址。範本範圍可為 `2000::/3` 內任意 `/3` 至 `/128`。 |
| 固定地址 | 從前綴範本建立的具名單一 IPv6，可自動生成或在進階模式手填；可作入站或固定出站。 |
| 動態入站池 | 包含多個 IPv6 的入站資源。使用它的節點會在池內全部目前地址的同一埠監聽。 |
| 共享出站池 | 可由多個節點共同使用的來源 IPv6 集合。每個新目的連線以 round-robin 選址，單條連線期間保持不變。 |
| 專用出站池 | 只屬於一個節點的來源 IPv6 集合；刪除節點時一併清理。 |
| 釘選地址 | 進階模式可把既有固定地址納入動態池。刷新時釘選成員保留，只替換自動生成的成員。 |
| wildcard | 監聽該位址族的所有本機位址，例如 IPv4 `0.0.0.0` 或 IPv6 `[::]`。wildcard 會涵蓋同位址族的具體位址，因此同埠設定須避免衝突。 |
| draining／排空 | 池刷新後，舊地址不再接新連線，但會保留到所有既有連線結束。管理頁可查看排空批次，必要時經二次確認強制終止。 |

### Linux IPv6 配置

| 名詞 | 說明 |
| --- | --- |
| `address` | 程式把每個 IPv6 以 `/128` 加到指定介面，等待 DAD 成功後才提交整批操作。適合一般已路由前綴。 |
| `local-route-freebind` | 程式建立整段 local route，不逐一加入地址；每個 socket 使用 freebind 綁定前綴內地址。適合大量地址且核心支援相關能力的環境。 |
| `external` | 地址與 route 完全由外部系統預先配置；程式只驗證可綁定，永不新增或刪除它們。 |
| DAD | Duplicate Address Detection，IPv6 地址啟用前的重複檢測。逐地址模式會等待完成；任一地址失敗或逾時會回滾整批操作。 |
| freebind | 允許 socket 綁定未逐一配置在介面上的本機 IPv6；本專案只在 `local-route-freebind` 模式使用。 |
| ULA | Unique Local Address，即 `fc00::/7` 私有 IPv6。目的地預設政策可全域設定，節點再以 inherit／allow／deny 覆寫。 |

### DNS、NAT64 與健康狀態

| 名詞 | 說明 |
| --- | --- |
| IPv6-only 出站 | 所有代理資料連線只建立 IPv6 socket，不會回退使用宿主機 IPv4。原生 IPv6 目的可直接連線。 |
| DNS64 | 目的名稱沒有可用 AAAA 時，程式以 IPv6 向 resolver 查詢 A，並把允許的 IPv4 嵌入目前 NAT64 `/96`。合成在本機完成。 |
| NAT64 | 網路側把合成 IPv6 轉換到 IPv4 目的的能力。DNS64 resolver 不等於 NAT64 gateway；管理頁會另外測試實際資料路徑。 |
| DoT | DNS over TLS。DNS 查詢固定使用 IPv6-only TCP/TLS，內建 Cloudflare 與 Google DNS64 resolver，可在進階模式調整順序或加入自訂端點。 |
| NAT64 前綴 | 用於嵌入 IPv4 的 canonical IPv6 `/96`。程式可自動探測；進階模式可手動覆寫。 |
| `healthy` | 核心服務與必要路徑正常。安裝器把它視為成功。 |
| `degraded` | 管理面與可用功能仍運行，但 NAT64、外部防火牆、個別資源或節點有問題。安裝器也把它視為可啟動狀態，應在管理頁查看細節。 |
| `unhealthy` | 核心服務不可用或無法安全提供管理能力。安裝器會把新版本視為失敗並執行回滾。 |

### 建議操作順序

1. 先到「IPv6 資源」建立前綴範本，再建立固定地址或池。
2. 到「節點」選擇入站模式、具名入站資源與具名出站資源，建立並啟動代理。
3. 到「網路」執行連通性測試，確認 DoT、原生 IPv6、NAT64 及各出站資源。
4. 回「總覽」確認健康與流量；出現錯誤時到「日誌」依節點、協定、動作或結果篩選。
5. 只有在安全預設不符合實際網路時才切換進階模式；變更前綴模式、NAT64、resolver 或限制前，先確認宿主路由與防火牆配置。

## 網路配置流程

1. 在管理頁建立命名前綴範本，填入已路由的 `2000::/3` 內 CIDR、Linux 介面與配置模式。
2. 建立固定 IPv6、動態入站池或共享/專用出站池。
3. 建立節點，選擇 IPv4、IPv6 或雙棧入站，以及具名 IPv6 入站/出站資源。
4. 在「網路」頁確認 DoT、原生 IPv6、NAT64 與各出站資源的連通性。
5. 檢查宿主機其他防火牆規則；管理頁的 degraded 診斷不會自動修改外部 table/chain。

三種前綴模式的詳細行為與適用情境請參考上方「Linux IPv6 配置」詞彙表。

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

## 授權

Copyright (C) s12ryt

本專案以 [GNU Affero General Public License v3 或任何後續版本](LICENSE) 授權，SPDX 識別碼為 `AGPL-3.0-or-later`。

你可以依授權條款使用、修改與散布本專案。若修改版本透過網路提供服務，必須依 GNU AGPL 第 13 條向該服務的使用者提供對應原始碼。第三方相依套件仍適用各自的授權條款。
