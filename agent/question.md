# s12ryt-ipv6 首版需求與驗收契約

更新日期：2026-08-03
狀態：使用者已確認，作為首版實作與驗收的唯一依據。

## 1. 產品範圍

- 從空工作區建立可多開節點的 IPv6 代理管理器。
- 後端採 Go 模組化單體；不得依賴 sing-box 等外部代理核心。
- 可使用必要 Go 函式庫。`github.com/things-go/go-socks5` 僅用於 SOCKS5 協商、認證與封包解析；TCP CONNECT 與 UDP ASSOCIATE 資料路徑由本專案接管。
- 管理介面採繁體中文 React + TypeScript + Vite SPA，產物由 Go embed，不需要 Node.js runtime。
- 首版不做設定匯入/匯出、SOCKS5 BIND、HTTPS 管理入口或自動 TLS。

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
