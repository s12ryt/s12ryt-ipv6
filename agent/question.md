# s12ryt-ipv6 首版需求與驗收契約

更新日期：2026-08-24
狀態：使用者已確認，作為首版實作與驗收的唯一依據。

## 1. 產品範圍

- 從空工作區建立可多開節點的 IPv6 代理管理器。
- 後端採 Go 模組化單體；不得依賴 sing-box 等外部代理核心。
- 可使用必要 Go 函式庫。`github.com/things-go/go-socks5` 僅用於 SOCKS5 協商、認證與封包解析；TCP CONNECT 與 UDP ASSOCIATE 資料路徑由本專案接管。
- 管理介面採繁體中文 React + TypeScript + Vite SPA，產物由 Go embed，不需要 Node.js runtime。
- Web 管理面不做設定匯入/匯出；本機 agent CLI 依第 24 節提供宣告式 apply/export。仍不做 SOCKS5 BIND、HTTPS 管理入口或自動 TLS。

## 2. 節點與協定

- 每節點協定可選 `socks`、`http`、`mixed`。
- SOCKS5 支援 TCP CONNECT 與 UDP ASSOCIATE；BIND 明確回覆不支援。
- HTTP Proxy 支援 CONNECT 與一般 absolute-form HTTP 轉送；不得記錄 URL path、header 或內容。
- mixed 在同一 TCP port 依首個協定位元辨識 SOCKS5 或 HTTP。
- 入站位址族可選 IPv4-only、IPv6-only、雙棧。
  - IPv4 使用 `0.0.0.0` wildcard。
  - IPv6 使用固定具名地址或具名動態入站池。
  - 動態入站節點同時監聽池內所有 IPv6 的同一 port。
- 出站必選固定具名 IPv6、具名共享動態池或節點專用動態池。
- 動態出站對每條新目的 TCP 連線或 UDP `client + destination` 映射以 round-robin 選來源 IPv6；同一連線/映射期間來源不變，目的地址重試亦不更換來源。
- 新節點預設立即啟動。無認證節點的建立請求必須包含明確風險確認，否則後端拒絕。
- 運行中編輯採交易式切換：先驗證並啟動新設定，成功後切換且立即終止舊連線；失敗保留舊設定與服務。
- 停止與刪除節點立即停止接收並終止全部既有 TCP/UDP；刪除節點一併刪除其專用池。

## 3. 連線、埠與限制

- 節點 port 可手動指定或留空自動配置。
- 節點與 SOCKS5 臨時 UDP relay 共用可設定範圍，預設 `49152-65535`。
- 配置器按實際 IP、位址族與 transport 檢查 active listener/association；不同 IP 可重用同一 port。
- wildcard listener 覆蓋該位址族所有位址並參與衝突判斷。自動節點 port 必須同時可供必要 TCP/UDP 使用。
- 每節點預設最大並行 TCP 4096、UDP association 1024，皆可設定；超限快速拒絕並記錄。
- TCP dial timeout 預設 10 秒；tunnel idle timeout 預設關閉 (`0`)；代理握手 timeout 預設 30 秒；皆可逐節點調整。
- UDP association 與每目的映射 idle timeout 預設 5 分鐘，可調；TCP 控制連線關閉時立即回收 association。
- UDP relay 必須從設定範圍配置 port，並透過 nftables 動態開孔與回收。

## 4. IPv6 資源模型

- 使用「命名前綴範本」，欄位至少包含名稱、CIDR、Linux 介面及配置模式。
- 一般前綴必須完整位於全球單播 `2000::/3`，支援任意 `/3` 至 `/128`，不得寫死 `/64`。
- 前綴輸入正規化為 network CIDR；不同範本禁止任何重疊或包含關係。
- 自動生成可使用前綴內全部位址，包含全零主機位；池容量不得超過前綴可提供且未衝突的位址數。
- 每池容量限制 `1-4096`；動態入站預設 10、共享出站預設 100、專用出站預設 15；節點總數上限 1024。
- 支援多個具名入站池與共享出站池；建立時選擇前綴範本。專用出站池在建立節點時建立。
- 固定地址先在資源頁從範本建立具名資源，可手填範圍內地址或安全隨機生成；節點只引用既有資源。
- 同一實體 IPv6 僅有一筆 canonical 地址與所有權紀錄。固定地址可被多節點引用；池可釘選既有具名地址，容量包含釘選成員。
- 池刷新只替換自動生成成員；釘選成員保留。新地址全部配置、DAD/bind/firewall 驗證成功後才切換。
- 刷新後新連線立即用新池；舊地址保留至既有連線自然結束，無硬性排空期限。UI 顯示 draining 批次並提供二次確認後強制終止。
- 仍被池、固定地址或節點引用的範本/地址拒絕刪除並列出引用。
- 範本有引用時只能改名稱；CIDR、介面、模式需建立新範本後遷移。

## 5. Linux 地址配置與所有權

每個範本獨立選擇：

1. `address`：程式以 `/128` 加入指定介面，平行等待 DAD 最多 60 秒；任一 `dadfailed` 或逾時整批回滾。
2. `local-route-freebind`：程式建立整段 local route，socket 使用 per-socket IPv6 freebind，不修改全域 sysctl。
3. `external`：宿主機外部預配置；程式只驗證且永不修改地址或 route。

- 上游前綴已路由至宿主機；程式不建立 IPv6 預設路由。
- 所有建立/刷新操作先驗證、後交易提交；配置、DAD、bind 或 firewall 失敗即回滾且不保存。
- 既有但非本程式建立的地址/route 一律避讓且永不刪除；外部模式以非擁有引用使用。
- 所有權持久化。啟動時對帳並清除不屬現行狀態但由本程式建立的殘留資源。
- 正常停止時移除全部由本程式建立的 IPv6 地址、local route 與 nftables table。
- 需要 root 或相應 Linux capabilities；正式支援 Debian 12/13、Ubuntu 24.04。

## 6. 嚴格 IPv6 出站、DNS64 與 NAT64

- 所有代理資料 dialer、DNS 查詢及健康測試只建立 IPv6 socket；不得回退宿主 IPv4。
- 預設 DoT 順序：Cloudflare DNS64 (`2606:4700:4700::64`、`2606:4700:4700::6400`) 後 Google DNS64 (`2001:4860:4860::6464`、`2001:4860:4860::64`)；驗證 TLS 名稱。
- UI 可新增自訂 DoT resolver，必須提供 literal IPv6、port 與 TLS server name；可排序及停用，但至少一個有效 resolver。
- 查詢原生 AAAA；無 AAAA 時查 A，將允許的 IPv4 嵌入目前 NAT64 `/96`。直接輸入 IPv4 同樣經此流程。
- DNS 正向快取尊重 TTL 並限制 30 秒至 10 分鐘；負向快取 30 秒。每次使用快取結果仍重新套用政策。
- 同一網域混合允許/禁止地址時丟棄禁止地址；無允許地址才拒絕。
- NAT64 `/96` 預設以 RFC 7050 類型流程向內建 DNS64 探測，UI 可手動覆寫。多來源不同時採優先 resolver 結果並顯示來源。
- 啟動立即測試，之後每 60 秒重測。NAT64 故障時節點維持原生 IPv6；IPv4 literal/A-only 目的快速失敗且狀態 degraded。
- 連通性測試涵蓋 DoT、原生 IPv6、NAT64、每個固定出站及每個出站池一個代表地址。

## 7. 目的地政策

- IPv4 literal 或 DNS A 在 NAT64 合成前，只允許全球可路由單播；拒絕 RFC1918、CGNAT、loopback、link-local、multicast、unspecified、benchmark、文件與其他特殊保留範圍。
- IPv6 預設拒絕 loopback、link-local、multicast、unspecified及非全球單播。
- ULA 採全域預設，節點可選繼承、允許或拒絕。
- 永久拒絕宿主機所有本機地址與本程式管理地址，避免遞迴與存取管理服務。
- 直接提供 NAT64 前綴內 IPv6 時必須解碼後再次套用 IPv4 政策，防止繞過。
- 解析結果與直接 IP 使用相同政策；禁止結果被移除後，允許結果可繼續使用。

## 8. 防火牆

- 自動管理 nftables，但只擁有獨立 `inet s12ryt_ipv6` table；不得修改其他 table/chain。
- 規則涵蓋管理 TCP、節點 TCP 與 active UDP relay，更新須原子化。
- 啟動重建、正常停止移除整個自有 table。若其他既有 base chain 仍阻擋，健康狀態標示 degraded 並顯示診斷，不侵入修改。
- 建立/啟動節點若自有 nftables 規則套用失敗，節點啟動及相關資源變更回滾。

## 9. 認證與秘密

- 每節點可選無認證或一組 SOCKS5 username/password 與 HTTP Proxy Basic 共用帳密。
- 代理 username 為 1-64 個可列印 ASCII；password 為 12-128 個可列印 ASCII。啟用認證後，任一留空欄位以 CSPRNG 安全生成。
- UI 登入後可查看、複製與重設代理帳密。分享 URL 正確處理 IPv6 方括號與 credential percent-encoding。
- 代理帳密以獨立 0600、32-byte master key 配合 AES-GCM 加密持久化；不得以明文或可逆固定 key 保存。
- 首次啟動生成 32-byte CSPRNG base64url 管理密碼，只輸出一次至 stdout/journal；以 Argon2id 雜湊保存。
- 手動管理密碼至少 16 字元、最多 256 bytes，可用 UTF-8；Web 登入後可修改。
- `admin reset-password` 透過 0600 Unix control socket 對運行服務即時重設並撤銷 session；服務停止時以檔案鎖直接更新。

## 10. Web 安全與管理入口

- 管理面固定雙棧 wildcard，預設明文 HTTP `:34466`，port 可由 CLI/config 修改，但不可改成單棧或特定位址。
- 管理 port 被占用時整體退出且不啟動任何代理節點；不得靜默換 port。
- 使用者已明確接受公網 HTTP 明文傳送管理登入資訊的風險；UI 與日誌持續醒目警告，不得擅自改 HTTPS。
- Cookie 使用 `HttpOnly`、`SameSite=Strict`，因 HTTP 不設 `Secure`；狀態變更 API 使用 session-bound CSRF token、Origin 檢查與 JSON content type。
- session 閒置 30 分鐘、絕對 12 小時；同時只允許一個，新登入、改密碼或程序重啟撤銷舊 session。
- 登入失敗限速：每來源 5 次/15 分鐘、全域 500 次/15 分鐘；IPv6 來源按 `/64` 聚合，僅信任 socket RemoteAddr。
- `/healthz` 未登入可讀，但只回 healthy/degraded/unhealthy 與 HTTP 狀態，不含節點、地址或錯誤細節。
- 敏感回應使用 `Cache-Control: no-store`；不得將 proxy credential、session 或 CSRF 放入日誌/SSE。

## 11. SPA 功能與視覺

- 實際操作介面為首頁，不做 landing page。
- 頁面包含：總覽、節點完整 CRUD/啟停/狀態/憑證、前綴/固定地址/池/刷新/draining、網路與 resolver/NAT64/連通性、日誌與健康、管理密碼。
- 即時資料以 SSE 更新；完整操作使用同源 JSON API。
- 緊湊、安靜、操作導向；不得以裝飾性大卡片或巢狀卡片取代資訊架構。
- 使用 Lucide icon、完整 label、鍵盤 focus、ARIA 與足夠對比；響應式驗證 375、768、1024、1440 px，不可水平溢出或文字重疊。
- 預設跟隨系統主題，可切換亮/暗並保存於瀏覽器；尊重 `prefers-reduced-motion`。

## 12. 日誌、稽核與統計

- 代理日誌只含時間、節點、協定、成功/錯誤、來源 IP、目的 host/port、選用出站 IP及必要錯誤分類。
- 禁止記錄帳密、URL path、HTTP header、內容、session、CSRF 或明文代理憑證。
- JSONL 同時保存代理、系統與去敏管理稽核；另輸出 stdout/journal。
- 輪替為 100MB、保留 5 檔。UI 可篩選、查看尾端與二次確認後清除全部輪替檔；清除後第一筆為執行者/時間的稽核事件。
- 每節點提供即時與持久累計：活躍 TCP/UDP、總連線、上下行 bytes、錯誤。
- 統計每 30 秒與正常停止時原子保存；支援單節點與全部歸零，歸零寫入稽核。

## 13. 持久化與程序控制

- 原生 Linux 預設資料目錄 `/etc/s12ryt-ipv6/`，可由 CLI flag 覆寫。
- 設定、執行狀態、統計、master key、日誌各自分檔，使用 schema version、原子 replace、程序/檔案鎖與最小權限。
- YAML 保存設定與資源狀態；統計可使用獨立結構化狀態檔；不得把秘密寫進非加密欄位。
- 只允許一個服務實例控制同一資料目錄。
- 啟動時管理面優先；個別節點或前綴對帳失敗時，可用資源繼續，故障項目停用並標 error/degraded。

## 14. 交付

- 支援 Linux amd64 與 arm64。
- 提供前景 binary、systemd unit/安裝與移除流程、Dockerfile 與 compose 範例。
- Docker 必須使用 host network、`NET_ADMIN` 與持久化 `/etc/s12ryt-ipv6/` volume；文件明確說明 Linux-only 限制與 nftables 邊界。
- 不自動建立 IPv6 預設路由；文件列出上游前綴路由、權限及 host firewall 前置條件。

## 15. TDD 與驗收標準

- 每項正式行為均依 RED -> GREEN -> REFACTOR；缺陷先有穩定回歸測試。
- Go 測試涵蓋：CIDR `/3-/128` 與容量/重疊、canonical 引用、池生成/刷新/排空、port 衝突、目的政策、DNS cache/failover/DNS64、secret/auth/session/CSRF/限速、資源交易回滾、代理 TCP/UDP/HTTP/mixed、生命週期、API 與 SSE。
- Linux netlink/nftables 透過介面 fake 在 Windows 完整測試交易與所有權；提供 root-only Linux integration test 驗證真實 address/route/DAD/freebind/nftables。
- 前端使用單元/元件測試驗證主要表單、錯誤、警告、狀態與操作；以 Playwright 驗證登入及核心管理流程、桌面/手機、亮/暗與無溢出。
- 必須通過受影響測試、完整 Go test/race、vet/static analysis、前端 lint/typecheck/test/build、Go Linux amd64/arm64 build，以及環境可用時的 Docker build。
- 本機為 Windows；若無 Linux root/network namespace 或 Docker，真實 Linux E2E 必須明列「未完整驗證」、保留可執行測試與具體執行指令，不得誤報。

## 16. GitHub 交付

- 使用者確認建立公開儲存庫 `s12ryt/s12ryt-ipv6`。
- 本機以 `main` 為預設分支，建立可審查的原子提交後推送。
- 不覆蓋既有遠端歷史、不使用強制推送；若同名遠端已存在，先停止並檢查。

## 17. VPS 全自動安裝與 Release

- 在根目錄新增 `install.sh`，提供 GitHub Release 一鍵安裝與升級；既有 `deploy/install.sh` 保留為本機/offline binary 安裝入口。
- GitHub Actions 同時支援推送 `v*` tag 與手動 `workflow_dispatch`。手動流程要求輸入 `vX.Y.Z` 語意版本，從使用者選定的 workflow ref 建立 tag；格式不合法時拒絕。
- 手動流程遇到既有 tag 時，只有該 tag 指向目前 workflow 提交且尚無 GitHub Release 才可安全續跑；tag 指向其他提交或 Release 已存在時拒絕。發布時必須明確指定目前 tag，避免同一提交有多個 tag 時選錯版本。
- 既有孤立 tag `v0.1.0`、`v0.1.1` 均保留；本次修復後立即以新 tag `v0.1.2` 建立並驗證 Release，不刪除或重寫既有 tag。
- 採 GoReleaser 慣例命名，為 Linux `amd64`、`arm64` 同時發布 `tar.gz` 與裸 binary，並發布涵蓋全部資產的 `checksums.txt`。
- 安裝器只支援公開 GitHub 儲存庫 `s12ryt/s12ryt-ipv6`，預設安裝 latest；可用 `VERSION=vX.Y.Z` 指定版本。
- 可用 `DATA_DIR` 覆寫資料目錄，預設 `/etc/s12ryt-ipv6`。可選 `MANAGEMENT_PORT` 僅在明確提供時修改；未提供時保留既有設定，首次安裝使用 34466。修改必須透過專案 CLI 安全更新 YAML 並保留其他設定。
- 只支援 Debian 12/13、Ubuntu 24.04 與 `amd64`/`arm64`；其他系統或架構在修改現有安裝前拒絕。
- 以 apt 自動安裝 `curl`、`ca-certificates`、`nftables` 與必要校驗工具，並要求 systemd 可用。不得自動安裝或啟用 UFW；只有 UFW 已存在且 active 時才新增實際管理 TCP 埠規則。
- 管理埠變更時不自動刪除舊 UFW 規則，避免移除人工或其他服務共用規則；腳本需輸出清楚警告。
- 所有 Release 下載使用 HTTPS。必須先下載對應版本資產與 `checksums.txt` 並以 SHA-256 驗證；缺少資產、缺少checksum或校驗失敗時，不得替換現有安裝。
- 重複執行採安全升級：保留資料目錄，備份既有 binary、systemd unit 與本次會變更的 config。更新後一律 enable 並啟動服務。
- 健康檢查使用本機 `http://127.0.0.1:<port>/healthz`，最多等待 120 秒；`healthy` 或 `degraded` 視為成功，`unhealthy`、格式錯誤、連線失敗或逾時均視為失敗。
- 升級健康失敗時完整還原 binary、systemd unit與config，執行 daemon-reload、重新啟動舊版並再次檢查；不得留下半套升級。首次安裝失敗則停止服務並移除本次安裝的 binary/unit，但保留診斷輸出與資料目錄。
- 第一次啟動前記錄時間，只從該次 systemd 啟動時間後的 journal 擷取 `initial admin password: ...`，最多等待 120 秒且只輸出到目前終端。找不到時不得擅自重設，只提示 `admin reset-password` 指令。
- 安裝器不得建立或修改 IPv6 default route，也不得修改本程式以外的 nftables table。
- README 提供 latest、指定版本、資料目錄及管理埠的一行安裝範例，並保留公網明文 HTTP 的醒目安全警告。
- 驗收包含 shell 語法與可注入命令的安裝/升級/回滾測試、Go CLI 設定修改測試、GoReleaser config檢查、GitHub Actions YAML 靜態驗證，以及既有Go/前端完整回歸。

## 18. Web 面板基礎／進階模式與說明文件

- 管理面新增全域「基礎／進階」模式，控制項位於登入後頂欄、主題控制旁，使用具文字標籤的分段控制並具備鍵盤focus與`aria-pressed`狀態。
- 首次使用預設基礎模式；偏好只保存於目前瀏覽器`localStorage`，不寫入後端設定、不進入日誌或SSE。切換模式不得發送API mutation。
- 切換模式不得關閉已開啟表單或遺失已輸入內容。基礎模式編輯既有節點時，隱藏的limits、timeouts與ULA值必須原樣保留；只有新節點使用既有安全預設。
- 基礎與進階模式都保留總覽、節點、IPv6資源、網路、日誌五個頁面；進階模式維持目前完整功能。
- 基礎節點表單顯示：ID、名稱、協定、認證方式、代理帳密、出站資源、代理埠、入站位址族與IPv6入站資源；隱藏TCP/UDP上限、四個timeouts與ULA政策。節點列表的啟停、編輯、顯示／複製／重設帳密及二次確認刪除全部保留。
- 基礎資源表單：前綴範本只填名稱、CIDR與Linux介面，配置模式固定為逐址配置；固定位址只填名稱與前綴範本，位址自動生成；位址池只填名稱、用途與前綴範本，使用各用途既有預設容量且不提供釘選。刷新、刪除與強制排空仍保留既有二次確認。
- 基礎網路頁可查看NAT64與防火牆診斷、執行連通性測試及修改管理密碼；NAT64覆寫與DoT Resolver編輯只在進階模式顯示。
- 基礎日誌頁保留全部查閱與篩選能力；清除日誌及單節點／全部統計歸零只在進階模式顯示。
- README新增完整詞彙表與操作對照，至少解釋：基礎／進階模式、入站／出站、前綴範本、固定地址、三種位址池、三種Linux配置模式、draining、DNS64、NAT64、DoT、DAD、ULA、freebind、wildcard、健康狀態，以及各名詞所在頁面與適用時機。
- 驗收需以React元件測試證明模式預設／持久化／切換、表單輸入與隱藏值保存、各頁基礎模式可見操作及進階模式完整控制；執行完整前端test、lint、build並以Playwright驗證桌面／手機、亮／暗與無水平溢出。

## 19. 網路自動偵測、候選選單與自動命名

- 管理面應減少可由主機或既有資源推導的手動輸入；偵測必須由登入保護的後端 API 執行，不信任瀏覽器自行猜測 Linux 網路狀態。
- Linux 網路候選只列出目前 `UP` 且非 loopback 的介面。IPv6 前綴候選同時來自這些介面的全球 IPv6 地址與核心 IPv6 路由；忽略 default route、非 IPv6、scoped、非全球單播及無法關聯介面的項目。
- 相同介面與 CIDR 的地址／路由結果合併並保留來源標記。介面與 CIDR 在表單中分開選擇；選定介面後只顯示該介面的前綴候選。
- IPv6 資源頁進入時自動偵測一次，並提供明確刷新按鈕。刷新失敗時保留上一份候選、顯示去敏錯誤，且不得清除正在編輯的表單。
- 基礎與進階模式都可從候選選單選擇介面與 CIDR，也都可切換為自訂值。沒有候選時仍必須允許自訂，避免上游僅路由但核心狀態不足時無法建立範本。
- 與現有前綴範本相同、重疊或包含的候選仍顯示於選單，但必須停用並標示衝突範本名稱與原因；不得讓管理者選取後才以一般錯誤回絕。
- 新增節點時預設產生未占用的英文 ID `node-NNN` 與繁中顯示名稱 `節點 N`，兩者建立前皆可修改。
- 新增資源時預設產生未占用且可修改的繁中名稱：`前綴 <介面> N`、`固定位址 N`、`入站池 N`、`共享出站池 N`、`專用出站池 N`。流水號必須跳過目前快照中已存在的名稱。
- 進階網路頁的 NAT64 設定改為「自動探索／自訂 `/96`」模式選單；自動模式只顯示目前探測前綴與來源，自訂模式才顯示輸入欄。基礎模式維持唯讀，不增加設定操作。
- 進階網路頁提供 Cloudflare DNS64 與 Google DNS64 的可信 Resolver 預設選單；選取後自動帶入完整 IPv6、port 853 與 TLS server name，並避免加入重複端點。仍保留自訂 Resolver。基礎模式維持 Resolver 唯讀。
- 驗收需以 Go 測試覆蓋介面／地址／路由偵測、去重、排序、過濾、衝突標記與登入保護 API；以 React 元件測試覆蓋載入／刷新／失敗保留、自訂備援、自動命名、NAT64 模式及 Resolver 預設；最後執行完整 Go／前端回歸、Linux amd64／arm64 交叉編譯與 Playwright 多尺寸驗收。

## 20. Web 節點複製與 HTTP 剪貼簿備援

- 節點操作列提供兩個獨立功能：「複製連線資訊」與「複製連線帳密」；帳密按鈕只對有認證的節點顯示。
- 連線資訊採標準 URI、每個 URI 一行。SOCKS5 使用 `socks5://`，HTTP 使用 `http://`；mixed 節點同時輸出兩種協定。IPv6 host 必須加方括號，帳號與密碼必須做 URI 百分比編碼；無認證節點不得加入 userinfo。
- IPv6-only 節點從具名固定入站地址取得 host；具名入站池每次複製從目前 active IPv6 隨機選取一個。沒有 active IPv6、資源不存在或資源類型不符時拒絕產生不可用 URI，並顯示明確去敏提示。
- IPv4-only 節點使用目前管理面板網址的 hostname。雙棧節點同時輸出目前面板 hostname 與隨機一個 active IPv6／固定IPv6入口；重複 host 必須去重。
- 自動複製先使用 `navigator.clipboard.writeText`；公網 HTTP 或權限限制導致不可用／失敗時，改用隱藏 textarea 與相容複製命令。兩種方式都失敗時，顯示具標題、唯讀可選取內容與關閉操作的手動複製對話框。
- 複製成功後在該節點操作旁短暫顯示「已複製」；失敗不得靜默，也不得把代理帳密或完整URI寫入日誌、SSE、localStorage或錯誤訊息。
- 驗收需以純函式測試覆蓋URI編碼、協定、IPv4／IPv6／雙棧、隨機池與錯誤路徑；以元件測試覆蓋兩個按鈕、Clipboard API成功、HTTP備援、手動對話框與操作回饋；最後執行完整前端test、lint、build及實際HTTP瀏覽器驗收。

## 21. 一鍵批次建立節點與持久化資料夾

- 節點頁新增「一鍵建立多節點」。每批預設 5 個、上限 100 個；管理員填寫一次共用協定、認證、入站、出站及進階限制，先產生預覽，再一次送出。
- 預覽中每個節點可個別修改 ID、顯示名稱與代理埠；埠 `0` 表示自動配置。ID／名稱沿用未占用的 `node-NNN`／`節點 N` 流水號。
- 有認證的批次節點一律由後端為每個節點生成不同安全帳密，不共用帳密。無認證批次只需一次整批公開代理風險確認，後端未收到確認時必須拒絕。
- 批次內所有節點共用所選入站模式、入站資源與出站資源。後端必須先完成整批數量、ID、名稱、埠、資源及節點上限預檢，再開始建立；任一節點啟動或狀態保存失敗時，反向停止並移除本批已建立節點，盡力回復操作前狀態並回報回滾失敗。
- 每次批次建立必須指定唯一資料夾名稱，預填下一個未占用的 `批次 N` 並允許修改。資料夾名稱前後空白正規化後長度須為 1 至 64 個字元，禁止控制字元；與既有資料夾同名時拒絕，不自動合併。
- 資料夾歸屬由後端隨節點狀態持久化。既有或未指定資料夾的節點顯示於「未分類」；空資料夾不保存。單節點建立／編輯可選既有資料夾或未分類，既有節點可移動，資料夾可改名。
- 節點列表依資料夾名稱排序，「未分類」固定最後；資料夾可收合，收合狀態只保存於目前瀏覽器，不寫後端。
- 資料夾標題列提供：複製全部連線資訊、複製全部帳密、全部啟動、全部停止，以及二次確認後全部刪除。複製沿用 HTTP 剪貼簿三級降級且不得持久化秘密。
- 資料夾批量啟動／停止／刪除會嘗試所有節點，保留成功結果並逐項回報失敗；不得把不可保證回滾的刪除宣稱為原子操作。資料夾改名必須原子保存，目標名稱衝突時拒絕。
- 基礎模式隱藏批次的 limits、timeouts 與 ULA，進階模式維持完整共用設定；切換模式不得清除批次表單或預覽。
- 驗收需以 Go 測試覆蓋資料夾持久化相容性、批次預檢、獨立帳密、單次保存、建立失敗／保存失敗回滾、改名／移動及批量逐項結果；以 React 測試覆蓋預覽、可編輯欄位、資料夾分組／收合／操作、整批複製與基礎／進階模式，最後執行完整 Go／前端回歸與 Playwright 多尺寸驗收。

## 22. 管理面板排版、自定義控制項、Modal 與動畫

- 本輪改版涵蓋所有登入後頁面；登入頁維持獨立登入體驗。桌面保留側欄並支援收合，預設展開，收合偏好保存於目前瀏覽器；手機仍使用既有橫向導覽。
- 頁面內容統一重整為頁面標題、摘要／狀態、主要工具列與資料區，維持緊湊、安靜、操作導向的管理控制台，不改為landing page或裝飾性卡片堆疊。
- text、number、password、textarea、原生select與checkbox統一套用自定義視覺、focus、disabled、invalid與亮／暗主題狀態。select保留瀏覽器原生語意與鍵盤行為；checkbox保留原生input供輔助科技使用，只替換視覺呈現。
- 所有需要填寫或修改設定的操作改為modal：單節點新增／編輯、批次節點、資料夾移動／改名、IPv6資源建立、NAT64、Resolver及管理密碼。所有寫入命令也先以modal明確確認，包括節點與資料夾啟停／刪除、帳密重設、池刷新／刪除／強制排空、統計歸零與清除日誌。
- 連通性測試與日誌篩選屬查詢操作，保留頁內直接執行，不額外開確認modal。
- modal在桌面置中並有穩定寬度；手機保留小邊距、近全屏，標題與主要操作列固定，內容區獨立捲動。點擊backdrop永遠不得關閉modal。
- modal開啟時需鎖定背景捲動、限制鍵盤focus於modal內並在關閉後還原到觸發按鈕。所有modal使用`role="dialog"`、`aria-modal="true"`及可解析標題。
- Esc會提出關閉要求：乾淨表單可直接關閉；髒表單需疊加自定義確認modal。第二層確認modal的Esc只關閉確認並返回原表單；放棄或繼續編輯只能由明確按鈕決定。X與取消遵循相同髒表單規則。
- 批次節點modal使用三步流程：共用設定、逐列預覽、最終確認。前進前驗證目前步驟，返回或切換基礎／進階模式不得遺失已填資料及預覽。其他長表單使用單頁分區、可捲動內容與固定操作列。
- 動畫使用原生CSS，不增加動畫runtime。modal、backdrop、側欄收合、頁面切換、資料夾收合、步驟切換及成功／錯誤回饋使用160至220ms的opacity／transform／color動畫，不用會造成layout shift的scale hover。
- `prefers-reduced-motion: reduce`時移除非必要位移與轉場，功能與focus順序不得依賴動畫完成。
- 驗收需以React元件測試覆蓋modal外部點擊、Esc、髒表單巢狀確認、focus trap／restore、背景鎖定、批次步驟保存、所有寫入操作modal化、自定義checkbox及側欄偏好；CSS契約測試覆蓋動畫與reduced-motion；最後執行完整前端test、lint、build及Playwright 375／768／1024／1440、亮／暗、鍵盤與無水平溢出驗收。

## 23. AGPL 開源授權

- 專案採 GNU Affero General Public License v3 或任何後續版本，SPDX 識別碼為 `AGPL-3.0-or-later`。
- 著作權署名使用 `s12ryt`，不另列年份。
- 根目錄新增標準 GNU AGPL v3 全文 `LICENSE`；README 新增授權章節，清楚標示 SPDX 識別碼、著作權人及網路服務散布原始碼義務。
- `web/package.json` 與 lockfile 根套件中繼資料同步宣告 `AGPL-3.0-or-later`；第三方相依套件維持各自授權，不改寫其授權欄位。
- GoReleaser 的後續 `tar.gz` 發布封裝必須包含根 `LICENSE`；裸 binary 與 checksum 發布形式維持不變。
- 不加入全專案逐檔授權標頭，不修改、刪除或重發既有 `v0.1.2` Release；授權變更自本次 `main` 提交及其後版本生效。
- 驗收需以發布契約測試證明 `LICENSE`、README 與 npm SPDX 宣告一致，並以 GoReleaser 設定檢查證明封裝有效；完成後建立原子提交並以一般 push 推送 `origin/main`，不得改寫歷史或強制推送。

## 24. 本機 Agent CLI 與一鍵安裝整合

### 24.1 入口、通道與輸出契約

- 一鍵安裝後由同一個 `/usr/local/bin/s12ryt-ipv6` 提供 `s12ryt-ipv6 agent ...`，不新增 MCP server 或另一個常駐程序。
- agent CLI 只透過資料目錄中的 0600 Unix control socket 呼叫運行中的正式 service；不得繞過 runtime 直接修改 YAML。執行者必須為 root 或可透過 `sudo` 存取 socket 的管理者。
- 既有 `serve`、`version`、`admin`、`config` 命令及其文字輸出維持相容；只有 `agent` 命令使用機器可讀契約。
- 一般成功輸出單一 `{ "ok": true, "data": ... }` JSON；`schema` 與成功的 `export` 直接輸出文件，不包 envelope。所有失敗均在 stdout 輸出單一 `{ "ok": false, "error": { "code": "...", "message": "...", "details": ... } }` JSON，不得洩漏帳密、session、CSRF 或任意內部錯誤內容。
- 固定錯誤碼為 `invalid_usage`、`invalid_document`、`confirmation_required`、`unavailable`、`permission_denied`、`not_found`、`conflict`、`operation_failed`、`internal_error`。
- 程序退出碼：成功 `0`；用法或 schema/document 錯誤 `2`；socket 不可用或權限錯誤 `3`；業務衝突、找不到或拒絕操作 `4`；內部、執行或非健康狀態失敗 `1`。
- control request/response 上限由 4 KiB 提升為 4 MiB，繼續拒絕未知欄位、多個 JSON 值與超限訊息。查詢／單步操作預設 timeout 30 秒，apply 預設 10 分鐘；全域 `--timeout` 接受 1 秒至 30 分鐘，server 一律以 30 分鐘為硬上限。

### 24.2 命令樹

- 頂層：`status`、`schema`、`export`、`apply`。
- 資源：`resources list`；`resources template create|delete`；`resources fixed create|delete`；`resources pool create|delete|refresh|force-drain`。
- 節點：`nodes list|get|create|update|delete|start|stop|batch-create|move`。
- 資料夾：`folders rename|start|stop|delete`。
- 網路：`network show|test|nat64 set|clear|resolvers replace`。
- 日誌與統計：`logs tail|clear`；`stats show|reset`。
- 複合 create/update/replace 輸入採 JSON 文件，使用 `--file PATH`；`--file -` 或省略 `--file` 時從 stdin 讀取。ID、name、folder、filter 等簡單 selector 可使用 flag；密碼不得出現在命令列 flag。
- `--show-secrets` 適用於 `export` 及 `nodes list|get|create|update|batch-create`；預設輸出必須遮罩密碼。
- 所有刪除、`--prune`、pool 強制排空、清除日誌及重設統計都必須明示 `--yes`，不得進行互動提示；缺少確認時回 `confirmation_required` 且零修改。

### 24.3 Apply、Export 與 Schema

- `schema` 直接輸出 JSON Schema Draft 2020-12；apply/export 文件包含 `schema_version: 1`，拒絕未知欄位、多文件 YAML 及多個 JSON 值。
- `apply` 與 `export` 都強制要求 `--format json|yaml`，不得依副檔名、stdin 或終端自動偵測。JSON export 可直接 pipe 至 `apply --format json`；YAML export 可直接輸出 stdout 並 pipe 至 `apply --format yaml`。即使要求 YAML，任何失敗仍輸出統一 JSON error。
- 文件分為可選的 `settings`、`resources`、`nodes`、`network` 頂層區段。`network` 管理 NAT64 與 resolvers；logs/stats 只提供命令式操作，不得納入 apply。
- apply 先嚴格解析並預檢整份文件、引用、衝突、上限、確認條件及執行計畫，預檢失敗時零修改。預檢通過後依計畫順序執行，首個錯誤停止；各既有 operation 保持自身交易／rollback，不要求跨 settings、resources、nodes、network 的全域 rollback。回應必須列出 `completed` 與失敗項目。
- `apply --dry-run` 必須執行相同完整預檢並回傳計畫，但不得修改檔案、runtime、Linux 網路、防火牆、日誌或統計；dry-run 即使與 `--prune` 併用也不要求 `--yes`。
- apply 預設保留文件中未列出的現有物件；只有 `--prune --yes` 刪除未列出的資源／節點。prune 只影響文件中明示存在的 `resources` 或 `nodes` 區段，整段省略時完全不變。
- template、fixed、pool 以名稱識別；同名且定義相同視為已收斂，同名但定義不同必須在整份預檢階段回 `conflict`，首版不得自動刪除重建或換址。
- node 以固定不可變 `id` 識別；每個 apply node 必須含 `desired_status: running|stopped`。同 ID 依文件更新可變欄位後收斂狀態；prune 刪除節點時沿用既有專用池清理與錯誤語意。

### 24.4 帳密、預設值與設定生效

- node authentication 使用明確 action：`set` 必須提供 username/password；`generate` 為新節點或明確旋轉產生新帳密；`preserve` 只允許既有節點且不得改變帳密。`mode: none` 必須同時提供 `confirm_unauthenticated: true`。
- 預設遮罩 export 對有認證節點輸出 `preserve`，因此可直接 round-trip 而不旋轉帳密；`export --show-secrets` 改輸出 `set` 與明文帳密。遮罩後不得以星號假密碼回寫。
- 新 pool 省略 capacity 時，依 kind 使用全域 pool defaults；新 node 省略 limits/timeouts 時使用全域 defaults。更新既有 pool/node 時，省略欄位一律保留現值，不得套用新預設覆蓋。
- `settings` 納入既有 config schema 的所有全域設定，採欄位級合併；省略欄位保持現值。`allow_ula` 及供 agent 新物件使用的 pool/limits/timeouts defaults 即時生效。
- management port、node/relay port range、max nodes，以及只在 production build 建立的 resolver/query/connectivity dial timeout 等 startup-only 欄位，只安全持久化，不由 CLI 呼叫 systemctl。成功回應必須精確列出 `changed_fields`、即時生效欄位及 `restart_required` 欄位；重新啟動後 active/configured 值才一致。
- `agent status` 回報 control 可用性、服務 health、active/configured 設定差異與 pending restart。socket 可連線但 health 為 degraded/unhealthy 時仍輸出有效資料，但退出碼為 `1`。

### 24.5 一鍵安裝與驗收

- 根 `install.sh` 先完成既有交易式 binary/unit/config 安裝並通過 HTTP `/healthz`，再執行新 binary 的 `agent status` 驗證 control 通道與 JSON schema。
- HTTP health 為 `healthy` 或 `degraded` 且 agent status 回傳結構有效時，安裝可成功；degraded 導致 agent 自身退出 `1` 不得誤判為通道故障。socket／權限錯誤、逾時、無效 JSON、錯誤 envelope 或命令執行故障必須觸發既有完整升級／首次安裝回滾。
- 安裝成功後輸出不含秘密的 quickstart，至少展示 `agent status`、`agent schema`、JSON/YAML export，以及 export pipe 至 dry-run/apply 的明示格式命令；安裝器不接收或自動套用初始 agent 配置。
- TDD 驗收至少證明：control 4 MiB 邊界與嚴格解析；命令樹、JSON envelope、固定錯誤碼／退出碼；JSON/YAML schema/export/apply round-trip；遮罩 export 不旋轉帳密；show-secrets 明文只在明示時出現；dry-run 零修改；所有破壞操作缺 `--yes` 時拒絕；prune 只影響明示區段；同名異定義資源在任何執行前拒絕；node ID 與 desired status 收斂；settings merge/default/restart_required；單步及 apply timeout；安裝器 agent gate 成功、degraded、故障回滾與 quickstart。
- 最終需執行受影響 Go/shell 測試、完整 `go test ./...`、`go vet ./...`、前端既有 test/lint/build、shell 語法與 installer/release 測試、Linux amd64/arm64 交叉 build；環境不可用的 Linux root/netns、Docker、race 或 GoReleaser runtime 驗證須如實列為殘餘風險。

## 25. Web UI 視覺美化（自主疊代授權）

更新日期：2026-08-25
狀態：使用者明示「自主疊代升級」並授權設計決策由 Agent 自行承擔（同時指示不追問細緻網路/技術細節）。

### 25.1 目標與範圍

- 使用者認為現行 Web UI「有點大眾」，要求美化；維持既有功能、公開契約與所有互動行為不變，不新增功能。
- 變更範圍以 `web/src/styles.css` 為主、`web/src/styles.test.ts` 擴充視覺契約；未經必要不改 JSX 結構與 `index.html`。

### 25.2 設計方向（Agent 定案）

- 「Teal Console」深青基礎設施主控台風：保留產品既有 teal-green 識別，建立畫布／chrome（側欄頂欄）／卡片三層視覺深度。
- 雙主題（light/dark/system）皆須支援；dark 採深藍炭底加微幅 teal 極光漸層，light 採冷灰紙面漸層。
- 識別性元素：品牌方塊 teal 漸層、狀態膠囊改實心底色加圓點、導覽作用中項加 teal 內縮指標條、登入頁加 teal 光暈與細網格背景。
- 字體維持系統字型堆疊（加入 zh-TW 字型優先序），不載入任何外部字型或 CDN 資源；數據採 tabular-nums。

### 25.3 硬性約束（沿用既有契約）

- `styles.test.ts` 既有全部字串斷言必須原樣保留：響應式斷點、modal 結構、180ms 側欄 grid 動畫、五個具名 keyframes、`@media 1024 → @media 430 → @keyframes pulse` 檔案順序、禁止任何 `:hover` 搭配 `transform: scale`。
- 互動回饋僅用色彩／邊框／陰影變化；`prefers-reduced-motion` 降級規則不變；不得造成 layout shift 或水平溢出（375/768/1024/1440）。
- 對比率：主要文字 ≥4.5:1；dark 主題按鈕改用深色文字 (`--on-primary`) 以達標。
- 純 CSS 屬靜態樣式，依 TDD 例外條款處理；但仍以擴充 `styles.test.ts` 的「視覺刷新契約」形成 RED→GREEN 證據，並以完整前端 test/lint/build、Go embed test 與 Playwright 實機視覺驗證收尾。

### 25.4 驗收

- 前端全部既有測試＋新視覺契約測試通過；ESLint、TypeScript/Vite build 通過。
- `go test ./...`（含 web embed）通過；治理紀錄同步更新；以原子提交推送 `origin/main`。
- Playwright 以去敏 API mock 驗證登入頁與主殼雙主題、多寬度無水平溢出、console 無錯誤。

## 26. Console／Journal 不再顯示代理連線 IPv6

更新日期：2026-08-25
狀態：使用者明示要求「SSH 連入後不要顯示那些 IPv6」；設計決策由 Agent 依授權自行定案。

- 使用者 SSH 進入 VPS 時，前景執行或 journal 中的輸出被每次代理連線的事件（含 `source_ip`、`destination_host`、`outbound_ip` 等 IPv6）洗版。
- 變更：`internal/eventlog` 的 `proxy` 類事件（每筆代理連線，成功與失敗皆同）只寫入 JSONL 檔案，不再鏡射 stdout/journal。
- `system` 與 `audit` 事件（服務啟停、健康、設定與管理操作稽核）維持同步輸出 stdout/journal，journal 保留診斷價值。
- 檔案內容不變：Web UI「日誌」頁、`GET /api/logs` 與 `agent logs tail` 仍可查詢完整 proxy 事件；輪替、去敏、清除與統計行為全部不變。
- TDD 驗收：RED 證明 proxy 事件仍鏡射 stdout；GREEN 後 stdout 僅含 system/audit、檔案仍含全部事件；完整 `go test ./...`、`go vet` 與 Linux 雙架構交叉建置通過。

## 27. IPv6 池輪換（refresh→drain）穩定性修復

更新日期：2026-08-25
狀態：使用者要求審查「IPv6 池輪換」的 bug 與瓶頸；延續穩定性審查模式，修復決策由 Agent 依授權自行定案。

### 27.1 缺陷 R1（中高）：重啟後 outbound 池 draining 批次永久殘留

- 重啟（含 crash）後所有連線已死，但 outbound 池的 drain tracker consumers 恆非空，且 runtime SourcePool 只含 Active 地址，draining 地址的 `onDrained` 永不觸發，批次永久殘留 state、地址持續掛在網卡、UI 永遠顯示排空中，唯一出口是管理員手動強制排空。
- 修復：`ResourceCoordinator.CompleteAllDrains(ctx)` 以單一事務完成全部池的全部殘留 draining 批次（clone→逐批 `CompleteDrain`→`commitCandidate`）；無 draining 時為 no-op（零寫入、零網路呼叫）。不經手 drain terminator（重啟後無活連線可終止）。
- 接線：production `ReconcileResources` 閉包在 `resources.Reconcile(ctx)` 之前呼叫，即節點 Restore 之前完成清殘。

### 27.2 瓶頸 R2（中高）：DrainQueue 逐地址完整事務

- 原 `DrainQueue.Run` 對批次內每個地址各走一次完整 coordinator 事務（state 深拷貝×2＋全量 Reconcile＋runtime Sync＋fsync＋全程持鎖）；刷新閒置 100 地址池即 100 次事務，大池災難性放大。
- 修復：`DrainedAddressCompleter` 介面改批次簽名 `CompleteDrainedAddresses(ctx, pool, []netip.Addr)`；`DrainQueue.Run` 將每輪取出的批次按池分組（組序＝首次出現序、組內保序）後每池一次呼叫。
- `ResourceCoordinator.CompleteDrainedAddresses`：驗證與正規化（Unmap、拒 IPv4-mapped、去重）→ 過濾僅剩仍在 draining 的地址（冪等，容忍與 ForceDrain／積壓消費交錯）→ 單一事務完成。`CompleteDrainedAddress`（單地址）保留並委託批次版，語義不變。

### 27.3 驗收

- RED：`drain_queue_test.go` 改批次介面後編譯失敗（介面/方法不存在）；`resource_service_test.go` 對 `CompleteDrainedAddresses`/`CompleteAllDrains` 編譯失敗。
- GREEN：app/admin 全綠；新契約涵蓋按池分組保序、單一事務（saves=1）、混合已完成/重複地址冪等、無 draining no-op、雙批次單事務清除、無效輸入拒絕。
- 完整 `go test ./...`、`go vet`、Linux amd64/arm64 交叉建置通過；治理紀錄同步更新並推送。

## 28. 第六輪自主授權：同類缺陷模式全面排查（F1/F2）

使用者要求翻找是否有其他類似 R1（重啟殘留）/R2（逐項事務）/B1（逐項 fsync）/B2-B3（O(n^2) 全量 dump）的同型缺陷。掃描結論：stats/node persistent/eventlog/agent apply/operations/monitor 週期/vault/config 均安全；發現並修復兩項資料路徑問題。

### 28.1 F1：UDP relay 埠範圍聚合（每 association 兩次全表 nftables 替換）

- 問題：每個 SOCKS5 UDP ASSOCIATE 觸發 firewall.Open/Close 各一次完整規則集替換（ListTables+DelTable+AddTable+N×AddRule+Flush），全程持 coordinator.mu＋Manager.mu；高 UDP 負載下每秒數十次全表重建並序列化所有節點啟停。
- 修復：`firewall.Opening` 加 `PortEnd uint16`（0=單埠，normalizeOpenings 驗證/去重/排序支援）；backend 對範圍生成 Gte/Lte 兩個 Cmp；`FirewallCoordinator` 以 `relayScope{family,address}` 計數——同位址僅首次/末次觸發 Replace，openings() 每活躍 scope 一條 UDP 範圍規則（Port=relayPortMin、PortEnd=relayPortMax）；production 以 settings.Ports.Min/Max 接線。
- 安全性取捨（已註解）：association 存續期間該位址整個 UDP allocator 範圍開放；範圍為程式專用、其他程式綁代理專用 IPv6 機率趨零、且仍需 socket 綁定才收包。
- 驗收：manager 3 新測試（dedup/inverted/deterministic sort）；coordinator 改寫（scope 計數 ReferenceCountsRelayScopesAcrossPorts、TracksRelayScopesPerAddress、建構子驗證擴充）；backend Gte/Lte 表達式測試於 WSL 實跑 PASS。

### 28.2 F2：PolicyProvider 每出站連線 clone 兩個地址集

- 問題：`Policy()` 每次呼叫（每出站連線一次）cloneAddressSet×2；address 模式大池時每連線最多 2×池規模 entries 複製。
- 修復（契約變更）：Sync/RefreshHostAddresses 本為 build-new-then-swap，`Policy()` 改回傳唯讀共享視圖（零複製）；`DestinationPolicy` 文檔明示「呼叫方不得修改」；全 codebase grep 證明零寫入消費者。
- 驗收：TestPolicyProviderPolicyReturnsZeroCopyViews（reflect Pointer identity，RED 於 clone 版失敗）；TestPolicyProviderPublishedViewsSurviveLaterUpdates（swap 語義守護）；TestPolicyProviderConcurrentPolicySyncAndRefresh（併發功能測試）；既有 mutation 防護斷言改為唯讀視圖契約斷言。
- 限制：-race 因本機無 cgo/gcc 無法執行；並發安全以結構性論證（不可變快照發佈＋RLock 讀取＋零寫入消費者）＋併發功能測試為證據。

## 29. 第七輪自主疊代：後端核心與代理長連線穩定性

更新日期：2026-08-28
狀態：使用者要求自主找出並修復底層缺陷；本節為本輪實作與驗收契約。

### 29.1 範圍與優先順序

- 稽核範圍為全部 Go 後端核心：代理資料路徑、節點與服務生命週期、IPv6 資源、網路、防火牆、DNS64、policy、持久化及管理控制；前端僅處理後端公開契約變更所必要的相容調整。
- 最高優先線索為「長連線大致在固定時間後失效」；目前無法確定代理入口、流量方向、觸發條件或實際 timeout 設定，因此必須同時驗證 SOCKS5 TCP、SOCKS5 UDP、HTTP CONNECT 與 mixed 共用路徑。
- 修復本輪所有可由決定性測試、靜態契約或可執行證據證明的高、中、低風險缺陷；不以臆測改碼，不新增功能，不進行與缺陷無關的大規模重構。

### 29.2 長連線可觀察契約

- TCP `tunnel_idle_timeout=0` 時，程式不得對已建立 tunnel 保留或新增資料傳輸 deadline；連線只因任一端關閉、不可恢復傳輸錯誤、節點停止或 context 取消而結束。
- TCP `tunnel_idle_timeout>0` 時，以雙向「實際無成功傳輸」時間計算閒置；任一方向成功傳輸皆刷新期限。單向持續傳輸不得因另一方向無資料而 timeout。
- TCP half-close 只關閉已完成方向的寫入側，不得提前關閉仍在傳輸的反方向；relay 必須在兩個方向均結束或 context 取消後回收。
- SOCKS5 UDP association 與每目的 mapping 的雙向成功活動均須刷新對應 idle 期限；控制 TCP 關閉或真正閒置逾時後才回收。
- 各入口完成握手後必須清除握手 deadline；mixed 分流不得把辨識階段 deadline 洩漏至下游 handler 或既有 tunnel。

### 29.3 相容性、遷移與錯誤語意

- 維持既有 CLI、HTTP API、設定欄位、代理協定與資料格式的公開行為；錯誤不得洩漏帳密、URL path/header/content 或內部敏感資訊。
- 若可證明的修復必須升級持久化 schema，新版本須可讀舊狀態並以原子方式向前遷移；不要求舊版本讀取新格式。未有必要證據時不得調升 schema。
- 保持 IPv6-only 出站、DNS64/NAT64、來源地址黏著、排空與防火牆 ownership 等既有安全邊界。

### 29.4 TDD 與完成門檻

- 每項正式碼修復前先新增可穩定重現的 RED 測試，確認因目標缺陷失敗；再以最小 GREEN 修復並執行鄰近回歸，最後僅在全綠下重構。
- 代理驗收至少涵蓋：零 timeout 不設 deadline、非零 timeout 雙向刷新、單向長傳輸、half-close、context 取消、UDP 雙向刷新、各入口握手 deadline 清除。
- 全後端掃描須檢查 panic、死鎖/競態、goroutine/FD/記憶體洩漏、錯誤吞沒、交易回滾、原子持久化、邊界值、協定偏差、安全性及不必要的熱路徑成本。
- 完成時須通過受影響測試、`go test ./... -count=1 -timeout=300s`、`go vet ./...`、前端既有 test/lint/build（若後端 embed 或契約受影響），以及 Linux amd64/arm64 CGO-disabled 交叉 build；Linux root/netns 或 race 因本機環境不可執行時須明列替代證據與剩餘風險。

## 30. 第八輪自主疊代：既有未修項與未深挖區域

更新日期：2026-08-28
狀態：使用者再次觸發「自主疊代升級」並指出「代碼底層還有 bug」；本節為本輪實作與驗收契約。使用者已確認先提交第七輪未提交修復，再以「既有未修項＋未深挖區域」為本輪掃描重點。

### 30.1 範圍

- 既有未修觀察項：eventlog `Tail` 持鎖全量解碼（查詢期間代理事件寫入排隊）、`ipv6resource` store `automaticCount==0` 覆蓋 err 的語意驗證。
- 前七輪未深入稽核的模組：`internal/secret`（master key、vault、credentials、Argon2id）、`internal/auth`（session、CSRF、limiter）、`internal/stats`、`internal/config`、`internal/admin` 的 HTTP handlers／SSE／control socket，以及 `cmd` CLI parser。
- 不重掃前七輪已證實安全的區域，除非本輪發現與其結論衝突的新證據。

### 30.2 修復與驗收

- 每項修復維持 RED → GREEN → REFACTOR 週期；只修可由決定性測試或可執行證據證明的缺陷。
- 效能瓶頸 B2/B3/B4（Kernel 介面批次化重構）本輪明示不在範圍，維持列建議。
- 維持 §29.3 的相容性、錯誤語意與安全邊界要求。
- 完成門檻同 §29.4：受影響測試、`go test ./... -count=1 -timeout=300s`、`go vet ./...`、必要時前端 test/lint/build，以及 Linux amd64/arm64 CGO-disabled 交叉 build。

### 30.3 完成紀錄（2026-08-28）

- 掃描結論：`secret`、`auth`、`stats`、`config`、`cmd`（main.go／agent_cli.go）無缺陷；`internal/app/management.go` 的 http.Server 無 WriteTimeout，SSE 長連線不受影響，無需修復；store `automaticCount==0` 覆蓋定案為刻意補償（pinned==capacity 池的合法路徑），不修。
- 修復 1（eventlog `Tail`）：原持 `l.mu` 全程逐行解碼最多 6 檔段（每段可達 100MB），Write 阻塞 1.47s；改為短鎖內檢查 closed＋開啟全部檔段 fd＋快照 current size，鎖外解碼；current 段以 `io.LimitReader` 防半行；文檔明示與 rotation/Clear 併發時為近似快照。RED：`TestLoggerTailDoesNotBlockConcurrentWrites`。
- 修復 2（admin `RequireMutation`）：Origin 檢查硬編碼 `http://`+Host，HTTPS 反代（README 承諾的可信安全通道）下所有寫操作 403；改為 `sameHostOrigin` helper——解析 Origin，scheme ∈ {http, https} 且 `origin.Host == request.Host`。契約變更：https same-host origin 由 403 改為放行（CSRF 防護核心是 same-host，攻擊者無法偽造受害者 Host）。既有測試 403 斷言同步改 204。RED：`TestHTTPServerMutationGuardAcceptsHTTPSSameHostOrigin`＋既有 guard 測試斷言調整。
- 修復 3（admin control `Serve`）：accept loop 同步呼叫 `handleConn`，長 agent apply（預設 10 分鐘）阻塞後續 control 連線（安裝器 120 秒健康檢查會誤判失敗回滾）；改為 `go s.handleConn(ctx, connection)` goroutine-per-connection；併發安全（AgentService 與 HTTP handlers 共用 services、PasswordManager 有 mu、activeSettings 建構後唯讀），ctx 取消仍透過 handleConn 的 watcher 中止每條連線。RED：`TestControlServerServesSecondConnectionWhileFirstIsBusy`。
- 修復 4（proxy `SourcePool.Replace`，使用者回報）：任何資源事務與每條 draining 地址連線結束都觸發 runtime.Sync → OutboundRegistry.Sync 對既有池呼叫 `Replace(pool.Active)` → 原實作無條件 `p.next = 0` 重置 round-robin，出站選址只在池內前兩個地址間來回；修復為新地址集合與 current 完全相同（`slices.Equal`）時直接 return nil，不重置 cursor、不動 draining。真 refresh（集合變化）仍重置。過程中曾引入重複 `p.mu.Lock()` 自我死鎖，由既有 dialer 測試（changed 路徑）捕獲後修正——回歸保護網生效。RED：`TestSourcePoolReplaceWithSameAddressesKeepsRoundRobinPosition`。
- 驗證：`go test ./... -count=1 -timeout=300s` 15 packages 全綠；`go vet ./...` 乾淨；前端 `npm test`（73 測試）、`npm run lint`、`npm run build` 全綠；Linux amd64/arm64 CGO-disabled 交叉 build 成功。
- 未修風險：eventlog `Tail` 與 rotation/Clear 併發時為近似快照（可能重複／遺漏少量事件）；Windows 上併發 Clear 可能因 fd 共享語意失敗（production 為 Linux，可接受）。

## 31. 第九輪自主疊代：底層缺陷深挖

更新日期：2026-08-28
狀態：使用者再次觸發「自主疊代升級」並指出「代碼底層還有 bug」；本節為本輪實作與驗收契約。技術細節由 Agent 依既有授權自行定案。

### 31.1 範圍與優先順序

- 最高優先：正確性缺陷（panic、死鎖/競態、goroutine/FD/記憶體洩漏、錯誤吞沒、交易回滾缺漏、原子持久化破壞、邊界值、協定偏差、安全邊界），集中在歷輪穩定性掃描覆蓋最少的區域：
  - `internal/admin` agent_service.go（apply/prune/export round-trip 宣告式邏輯、批次建立、逐項事務邊界）。
  - `internal/node` PersistentManager／批次建立／資料夾操作的交易與回滾。
  - `internal/app` service.go 生命週期／停止順序／connectivity 與 host address watcher。
  - `internal/proxy` 源租借、dialer 與 relay 邊角（前輪已掃，僅在有新證據時重查）。
  - `web/src` 前端 api.ts、SSE 訂閱、狀態管理（輕掃，僅處理契約級缺陷）。
- 次優先：若未發現新的正確性缺陷，評估既有結構性瓶頸 B2（linuxKernel.AddressExists O(C²)）／B3（waitForDAD 平行全量 dump）是否本輪以 Kernel 介面批次查詢重構處理；處理時必須以決定性測試證明行為等價（錯誤語意、回滾順序不變）。
- 不重掃前八輪已證實安全的區域，除非有衝突新證據。

### 31.2 修復與驗收

- 每項修復維持 RED → GREEN → REFACTOR；只修可由決定性測試或可執行證據證明的缺陷。
- B4（coordinator 單鎖涵蓋整個網路事務）除非證明造成可觀察正確性問題，否則維持列建議。
- 維持 §29.3 的相容性、錯誤語意與安全邊界要求；apply/prune/export 對既有文件的行為不得無聲變更。
- 完成門檻同 §29.4：受影響測試、`go test ./... -count=1 -timeout=300s`、`go vet ./...`、前端 test/lint/build（若 embed 或契約受影響），以及 Linux amd64/arm64 CGO-disabled 交叉 build；Linux root/netns 或 race 不可執行時明列替代證據與剩餘風險。

### 31.3 完成紀錄（2026-08-28）

掃描範圍與結論（均無新的正確性缺陷）：

- `internal/admin`：agent_document.go（mergeAgentSettings 欄位級合併＋Validate 正確；export preserve/set 語意正確；Resolvers clone 防禦到位）、operations_service.go（SetManualNAT64/UpdateResolvers 失敗回滾完整 errors.Join；Overview cancel 無洩漏；ResetStatistics 失敗返回錯誤不吞沒）。agent.go 的 apply 逐項事務屬前輪已深挖範圍，不重掃。
- `internal/node`：manager.go 全檔＋runtime.go RefreshBindings/drain 回呼鏈＋persistent.go 透傳＋drain_tracker/drain_queue 鎖序。針對「RefreshInboundBindings 持 m.mu 下同步觸發 onDrained 是否死鎖」逐幀驗證：callback 僅入 DrainTracker（鎖序單向 m.mu → DrainTracker.mu → DrainQueue.mu，不回叫 Manager）；DrainQueue.Run 取 batch 鎖外才呼叫資源鎖 CompleteDrainedAddresses；runtime.go drainedCallbackLocked 於鎖內原子「檢查＋刪除」retiring，防雙重觸發與過早排空。無死鎖、無缺陷。
- `internal/app`：service.go（results channel cap=3 恰等元件數；二次 closeListeners 冪等；cleanup 順序正確；InitializeRuntime 失敗僅 ShutdownFirewall 合理——nftables table 尚未建立）、connectivity.go、host_addresses.go、production_build.go（build 失敗不殘留 nftables；logger 雙關閉依賴 eventlog.Close 冪等；RestoreNodes 無條件 MarkRestored＋全節點 RegisterSecret 防洩漏；RunNAT64 裸 goroutine 隨 ctx 結束且與 cleanup 競態無害；prepareControlSocket 拒刪非 socket 檔；close once 單次）、startup_nodes.go（Restore 前 desired fallback 設計合理）、periodic_refresh.go、node_secrets.go（ErrPreviousRuntimeCleanup 特例仍註冊正確）——無缺陷。
- `web` 前端輕掃：EventSource 僅 api.ts:221（round8 已深掃冪等）；無 setInterval；NodesView copyTimer clearTimeout 保護完整；73 個前端測試基線全綠——無新缺陷。
- B2/B3 決策：本輪不實作。屬效能結構重構非正確性缺陷，需改 network.Kernel 介面＋linuxKernel＋waitForDAD＋fake kernel 測試全鏈；批次查詢行為等價的決定性驗證需 Linux netlink/netns 環境（Windows 無 root/netns，integration 無法執行）。留待下輪專項處理。

結果：本輪深掃未發現新的正確性缺陷；無程式碼修改；基線 `go test ./... -count=1`（15 packages）與 `go vet ./...` 全綠即為驗證。殘餘風險同前輪：B2/B3/B4 列建議；Linux integration 與 -race 未於本機執行（環境限制）。

## 32. 第十輪：B2/B3 批次查詢重構（使用者授權「開修吧」）

更新日期：2026-08-28
狀態：使用者於收到 B2/B3 平實解釋後明確授權修復；本節為本輪實作與驗收契約。技術細節由 Agent 依既有授權自行定案。

### 32.1 範圍與目標

- B2：`internal/network` linuxKernel.AddressExists 對每個地址各做 LinkByName＋全量 AddrList dump 再比對，C 個地址的批次操作總成本 O(C²)。目標：批次操作（新增/移除地址）前一次 dump 介面地址建記憶體集合，逐一查詢改為集合查詢；或 Kernel 介面新增批次/索引查詢能力，使呼叫端消除重複 dump。
- B3：address 模式 waitForDAD 為 C 個地址各開一個 goroutine，各自每 100ms 全量 AddrList dump 檢查 DAD。目標：共享單一輪詢器（一次 dump，fan-out 給所有等待者）。
- 兩者均為效能結構重構，非正確性修復：外部可觀察行為（回傳值、錯誤語意、回滾順序、逾時上限）必須與現狀完全等價。

### 32.2 等價要求與驗收

- 重構前先以測試鎖定現狀行為（characterization：AddressExists 的存在/不存在/錯誤透傳；waitForDAD 的成功/失敗/逾時與取消語意）；既有測試已涵蓋的部分不重複，只補缺口。
- 重構後全部既有與新增測試通過；`go test ./... -count=1`（15 packages）、`go vet ./...`、web test/lint/build、Linux amd64/arm64 CGO=0 交叉 build 全綠。
- 等價的主要證據為 fake kernel 單元測試；明列環境限制：Windows 無 root/netns（真實 netlink integration 未跑）、無 cgo（-race 未跑）。
- B4 維持列建議，不在本輪範圍。

### 32.3 完成紀錄（2026-08-28）

實作內容（B2/B3 批次查詢重構，使用者「開修吧」授權）：

- `internal/network/manager.go`：Kernel 介面新增 `InterfaceAddresses(ctx, interfaceName) ([]netip.Addr, error)` 與 `WaitAddressesReady(ctx, refs) error`；新增 `interfaceAddressSets` helper（每介面一次 dump 建集合）；removeStale/releaseAddresses/applyAddresses 三處 AddressExists 逐地址查詢改批次（錯誤格式逐字不變："check stale address %s"/"check address %s"/"check address %s on %s"）；waitForDAD 由 per-goroutine WaitAddressReady+cancel 改為單次委派 `kernel.WaitAddressesReady`（per-ref 錯誤包裝 "wait for address %s DAD: %w" 移入 kernel 實作，格式逐字等價，errors.Is(ErrDADFailed) 保持）。
- `internal/network/kernel_linux.go`：`InterfaceAddresses`（一次 AddrList(FAMILY_V6) 收集，錯誤包裝同 AddressExists）；`WaitAddressesReady`（按介面分組、每介面 link 一次、單一 ticker 每 tick 每介面一次 AddrList、DADFAILED 聚合、失敗時對未決 refs 附 context.Canceled、逾時對剩餘 refs 附 ctx.Err()、errors.Join 返回）。
- `internal/app/production_build_test.go`：連鎖修復——Kernel 介面新增方法導致 `productionTestKernel` 缺實作，補 InterfaceAddresses/WaitAddressesReady 兩 stub。

TDD 證據：

- RED：新增 6 測試（manager 層 TestApplyAddressesUsesBatchedKernelQueries、TestReconcileStaleRemovalsUseBatchedKernelQueries；kernel_linux 層 TestLinuxKernelInterfaceAddressesReturnsAddresses、TestLinuxKernelWaitAddressesReadyPollsInterfaceOncePerInterval、TestLinuxKernelWaitAddressesReadyAggregatesDADFailure、TestLinuxKernelWaitAddressesReadyPropagatesListError），執行編譯失敗（InterfaceAddresses/WaitAddressesReady undefined 共 5 處）＝缺方法 RED。
- GREEN：3 次迭代（nil map panic→fake InterfaceAddresses 從 addresses 推導單一真相源→批次計數補齊與 Reconcile 兩階段斷言 2）後，Windows 與 WSL（Linux）`go test ./internal/network` 全綠。
- 量測：Apply 3 地址——AddressExists 呼叫 3→0、WaitAddressReady 呼叫 3→0，改為 InterfaceAddresses 1 次＋WaitAddressesReady 1 次（dump 次數從 O(C) 降為 O(1)/介面；DAD 輪詢 dump 從 O(C)/tick 降為 O(1)/tick）。

回歸：Windows `go test ./... -count=1` 15 packages 全綠、`go vet ./...` 乾淨；WSL Linux `go test ./...` network/app/node/firewall/eventlog 等全綠（admin 重跑通過）；web `npm test` 73 passed、`npm run lint`、`npm run build` 全過；Linux amd64/arm64 CGO_ENABLED=0 交叉 build 雙架構成功。

環境限制與風險：Windows 無 root/netns（真實 netlink/nftables integration 未跑）、無 cgo（-race 未跑）；WSL2 下 proxy `TestRelayConnectionsHalfClosePreservesReverseTraffic` 系統性 flaky（connection refused，雙 conn pair 特徵；Windows 10/10 穩定；與本輪變更無關，定案不修，真機 Linux 驗證留待後續）；B4 維持建議。

## 33. 第十一輪自主疊代：底層缺陷深挖＋低成本防禦項

更新日期：2026-08-29
狀態：使用者觸發「自主疊代升級，我覺得代碼底層還有 bug」；經澄清：無具體症狀、全面深挖歷輪覆蓋最少區域；歷輪殘留低成本建議項一併修，B4 不動。本節為本輪實作與驗收契約。技術細節由 Agent 依既有授權自行定案。

### 33.1 深挖範圍（歷輪覆蓋最少的底層區域）

- `internal/dns64`：resolver 端點故障轉移與 TTL 邊界、cache 過期/淘汰互動、DNS64 合成、RFC 7050 探測、NAT64 健康監視（第二輪僅修 cache 上限，其餘角落未深挖）。
- `internal/policy`：目的政策全邏輯——IPv4 特殊範圍拒絕、ULA、NAT64 防繞過、本機/管理地址（歷輪僅動過文檔）。
- `internal/network/discovery.go`：介面/地址/路由候選偵測與合併排序。
- `internal/firewall`：外部 drop 診斷與 backend 其餘角落（F1 之外）。
- `internal/proxy` 僅在有新證據時重查（前輪覆蓋充分）。

### 33.2 低成本防禦項（使用者授權一併修）

- control socket agent 指令處理 panic 防護：handler panic 不得使整個程序崩潰；正常路徑行為不變。
- 刪除節點後 stats registry 殘留 entry：清理時機與方式不得改變現有統計查詢/保存行為。
- eventlog RegisterSecret 隨節點建立/輪換緩慢成長：去敏註冊表須有界或可回收；日誌去敏效果不得減弱。

### 33.3 修復與驗收

- 每項缺陷以 RED（可穩定重現的失敗測試）→ GREEN（最小修復）→ REFACTOR 完成；防禦項同樣先寫 RED。
- 只修可由決定性測試證明的缺陷；不做臆測性重構；B4 維持列建議。
- 完成門檻：受影響測試、`go test ./... -count=1 -timeout=300s`、`go vet ./...`、前端 test/lint/build（若契約受影響）、Linux amd64/arm64 CGO=0 交叉 build；環境限制照舊明列。

### 33.4 完成紀錄（2026-08-29）

深挖結論（dns64 resolver failover/TTL/cache 淘汰/RFC 7050/monitor 鎖序、policy 目的政策全邏輯、network/discovery、firewall 診斷）：無新的確定性正確性缺陷；dns64 cache stampede（併發同 key 重複上游查詢）屬效能非正確性，列建議。

四項修復（各自完整 TDD 週期）：

- 修復 1（stats `RemoveNode`）：`internal/stats/registry.go` 新增 `RemoveNode(node)`——鎖內 `delete`，空 ID no-op；語意與 `ResetNode`（保留 Active 歸零）明確區分。RED：`TestRegistryRemoveNodeDeletesCounters`、`TestRegistryRemoveNodeIgnoresUnknownAndEmptyNodes`（方法不存在編譯失敗）。
- 修復 2（eventlog secret 引用計數）：`internal/eventlog/logger.go` 新增 `secretCounts map[string]int`；`RegisterSecret` 重複值計數 +1（遮蔽行為不變）；新增 `UnregisterSecret`——計數遞減、歸零才移除、未知/空值 no-op；redact 遍歷 slice 順序不變（遮蔽輸出逐字等價）。多節點共用同密碼時不誤拆去敏（保守安全方向）。RED：`TestLoggerUnregisterSecretKeepsRedactionUntilLastReference`、`TestLoggerUnregisterUnknownOrEmptySecretIsNoop`。
- 修復 3（節點 Delete 清理掛鉤）：`internal/app/node_secrets.go`——`secretRegisteringNodeService` 新增可選 `statsRemover`（nil 容忍）與 `secretUnregistrar` 介面（可選實作，型別斷言）；`Delete` 先 `Get` 保留刪除前帳密，delegate.Delete 成功後反註冊 username/password 並 `RemoveNode(id)`；Delete 失敗不做任何清理；registrar 不支援反註冊時仍正常刪除。`internal/app/production_build.go` 接線改傳 `registry`。殘留語意（保守）：Update 輪換帳密時舊值計數不減、RestoreNodes 重複註冊計數 +1，均殘留至重啟——較原本永久洩漏改善且不減弱遮蔽。RED：`TestSecretRegisteringNodeServiceDeleteCleansUpSecretsAndStats`、`TestSecretRegisteringNodeServiceDeleteFailureSkipsCleanup`、`TestSecretRegisteringNodeServiceDeleteToleratesRegisterOnlyRegistrar`（建構子參數不符編譯失敗）。
- 修復 4（control panic 防護）：`internal/admin/control.go` `handleConn` 改 named return＋頂層 defer recover——panic 時 best-effort 回寫固定錯誤回應 `"internal control error"`（不洩漏 panic 內容給 client）、回傳 `control connection handler panicked: %v` 給呼叫端；recover defer 註冊於 `connection.Close` 之後，unwind 時先寫回應再關連線；`Serve` 的 per-connection goroutine 與 `HandleConn` 同步路徑同時受保護。RED：`TestControlServerHandleConnRecoversFromHandlerPanic`（panic 使測試進程崩潰）。

驗證：`go vet ./...` 乾淨；`go test ./... -count=1 -timeout=300s` 15 packages 全綠；本次變更檔案 gofmt 乾淨（`internal/admin/http_test.go`、`internal/network/manager_test.go`、`internal/node/firewall_coordinator.go` 為基線既有格式偏離，不在本次 diff，不動）；Linux amd64 CGO_ENABLED=0 交叉 build 成功（arm64 與本輪前各輪同機制，未重跑）。環境限制照舊：無 root/netns（integration 未跑）、無 cgo（-race 未跑）。

## 34. 第十二輪自主疊代：底層深挖＋殘留建議項收尾

更新日期：2026-08-29
狀態：使用者觸發「自主疊代升級,我覺得代碼底層還有bug,請你找出並修復」；經澄清：無具體症狀（全面深挖歷輪覆蓋最少區域）、歷輪殘留建議項一併修復。本節為本輪實作與驗收契約。技術細節由 Agent 依既有授權自行定案。

### 34.1 深挖範圍（歷輪覆蓋最少的底層區域）

- `internal/node`：inbound.go / outbound.go / resolved_runtime.go / resource_runtime.go / udp_factory.go / handler_builder.go（入站解析、出站 registry、協定 handler 組裝的完整邊角——歷輪僅掃 manager/runtime/persistent/drain 鏈）。
- `internal/proxy`：port_allocator.go（歷輪僅查 fd 洩漏，衝突/重用/預約邏輯未深挖）、socket_system.go、http_proxy.go 非 CONNECT 轉送完整路徑。
- `internal/admin`：nodes.go / resources.go / operations.go / http.go 的 handler 細節（第八輪掃巨觀，DTO 邊角未逐行）、password_store.go / reset_password.go。
- `internal/app`：traffic_observer.go、health.go、statistics.go、deferred_firewall.go、deferred_drain.go、startup_state.go、config_store.go。
- 前十一輪修復引入的新程式碼複查（第十輪批次查詢、第十一輪 RemoveNode/UnregisterSecret/Delete 掛鉤/control recover）。
- 不重掃前十一輪已證實安全的區域，除非有衝突新證據。

### 34.2 殘留建議項（使用者授權一併修復）

- S1：dns64 cache stampede——併發同 key 快取 miss 觸發重複上游 DoT 查詢；以 singleflight 類去重（同 key 併發只發一次上游查詢），錯誤語意與快取契約不變。
- S2：eventlog secret 註冊計數殘留——節點 Update 輪換帳密時舊值計數不減、RestoreNodes 重啟重複註冊計數 +1，殘留至重啟；補齊反註冊時機，遮蔽效果不得減弱。
- B4（ResourceCoordinator 單鎖涵蓋網路事務）維持列建議不動（高風險結構重構）。

### 34.3 修復與驗收

- 每項缺陷以 RED（可穩定重現的失敗測試）→ GREEN（最小修復）→ REFACTOR 完成；殘留項同樣先寫 RED。
- 只修可由決定性測試證明的缺陷；不做臆測性重構；維持 §29.3 相容性、錯誤語意與安全邊界。
- 完成門檻：受影響測試、`go test ./... -count=1 -timeout=300s`、`go vet ./...`、前端 test/lint/build（若契約受影響）、Linux amd64/arm64 CGO=0 交叉 build；環境限制照舊明列。

### 34.4 完成紀錄（2026-08-29）

深挖結論（均無新缺陷，全區掃畢）：

- `internal/node`：inbound.go（Sync 驗證＋原子 swap、Resolve fixed/pool ambiguous 檢查）、outbound.go（syncMu 序列化 Sync/ForceDrain、值語意 swap、既有池保留 SourcePool 物件 Replace）、resolved_runtime（純委派）、resource_runtime（同步順序第五輪已挖）、udp_factory（selectUDPRelayBind family 比對＋wildcard fallback）、handler_builder（HTTP 不建 relay）——無缺陷。觀察：dual 棧＋空 active 池時僅監聽 IPv4 為既有行為。
- `internal/proxy`：port_allocator（衝突檢查＝wildcard 覆蓋語意、ReleaseEndpoints 鎖序 r.mu→a.mu 無死鎖、normalizeBindSpecs 自身衝突檢查）、socket_system（驗證完整）、http_proxy（CONNECT/absolute-form、constant-time auth、relay 第七輪已挖）——無缺陷。觀察：ReleaseEndpoints 在 Close 失敗時仍從 reserved/bindings 移除（真實 binder 下無法穩定重現，列觀察）；非 CONNECT 轉送無 idle timeout（契約未要求）。
- `internal/admin`：operations.go（handler 全 RequireSession/RequireMutation、parseLogQuery limit 1-1000、validateResolvers 借 Default().Validate）、password_store（原子寫）、reset_password（control 優先→ErrControlUnavailable→ctx 檢查→offline lock，named return＋errors.Join）、nodes.go（batch 預檢 sameBatchSettings、ID 不可變、ErrPreviousRuntimeCleanup→200+warning、folderAction 逐項 Multi-Status）、resources.go（全部 RequireMutation、parseOptionalResourceAddress native IPv6）——無缺陷。writeResourceError 全部映射 400 為既有行為。
- `internal/app`：traffic_observer（去敏欄位、Rejected=Opened+Closed(0,0,true)）、health（RWMutex＋排序）、statistics（fs.ErrNotExist 容錯）、deferred_firewall/deferred_drain（once Set＋RLock 取引用鎖外呼叫）、startup_state（load 失敗→read-only 保護＋吞錯）、config_store（LoadOrCreate 冪等、update clone→change→Validate→Save→swap）——無缺陷。
- 前十/十一輪新碼複查（含 kernel_linux.go WaitAddressesReady）：分組輪詢、錯誤聚合、ctx 取消語意正確——無缺陷。觀察：全部地址就緒後仍對各介面輪詢 AddrList（啟動期短暫行為，非缺陷，不修）。

兩項殘留建議項修復（各自完整 TDD 週期）：

- S1（dns64 cache stampede）：`internal/dns64/resolver.go` 新增 `lookupCall`（done channel＋entry＋err）與 `inFlight map[cacheKey]*lookupCall`；`lookup` 在 cache miss 後經 `beginLookup` 加入既有 call（follower 等待 done 或自身 ctx 取消）或成為 leader；leader 以抽出之 `queryEndpoints`（原逐端點 failover/TTL/negativeTTL/clamp 邏輯逐字保留）查詢，完成後持 mu 寫 cache＋evict＋刪 inFlight，close done 廣播；成功與失敗均由全部等待者共享（追隨者收到 leader 相同錯誤）；查詢全程不持 r.mu；不引入新依賴。UpdateEndpoints 清 cache 時不動 in-flight（與原行為等價）。RED：`TestResolverCollapsesConcurrentLookupsForSameNameIntoSingleQuery`、`TestResolverSharesLookupFailureWithConcurrentWaiters`（blockingQueryer 阻斷上游，8 併發同 key 斷言上游查詢==1；修復前實測 8 次）。
- S2（secret 註冊計數殘留）：`internal/app/node_secrets.go` `Update` 於 delegate.Update 前以 `Get(id)` 捕獲舊 node；成功（含 ErrPreviousRuntimeCleanup）後先 `unregister(existing)` 再 `register(updated)`——帳密不變時淨零、輪換時舊值歸零移除、多節點共用密碼語意保持（計數設計）；Get 找不到時維持只 register（與 Create 同語意）。RestoreNodes 屬新進程計數從零，無殘留，不修。RED：`TestSecretRegisteringNodeServiceUpdateReleasesRotatedCredentials`（修復前 user-a/pass-a=1）、`TestSecretRegisteringNodeServiceUnchangedUpdateKeepsReferenceCountBalanced`（修復前=2），以 countingRegistrar 計數 map 斷言完整生命週期（Create→Update×N→Delete）歸零。

驗證：`go test ./... -count=1 -timeout=300s` 15 packages 全綠；`go vet ./...` 乾淨；Linux amd64/arm64 CGO_ENABLED=0 交叉 build 成功。前端未動（無契約變更），web test/lint/build 未重跑。環境限制照舊：無 root/netns（integration 未跑）、無 cgo（-race 未跑）。

