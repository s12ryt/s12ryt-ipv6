# 專案操作記錄

## 2026-08-03

- 確認工作區已被使用者清空，舊專案分析不再是實作依據。
- 確認本機工具：Go `1.26.3 windows/amd64`、Node.js `24.11.1`、npm `11.7.0`、Python `3.13.13`。
- 完成多輪需求澄清；使用者確認首版契約，詳見 `agent/question.md`。
- 查閱 `things-go/go-socks5` 原始碼：內建 UDP relay 使用核心分配 port 且沒有每映射 idle 回收；決定僅沿用協商/認證/封包解析，以公開 custom handler 實作 TCP/UDP 資料路徑。
- 選定模組化 Go 單體、React + TypeScript + Vite SPA、同源 JSON API + SSE、Linux 網路基礎設施介面化的架構方向。
- 建立 Go CLI 與 React/Vite 測試骨架。RED 證據分別為缺少 `run` 與缺少 `App`；GREEN 後 Go 2 項、前端 1 項測試通過，前端 lint/build 通過。
- `web/go.mod` 僅作工具邊界，避免根模組 `go test ./...` 掃描 `node_modules` 中第三方 Go 範例；前端不在執行時依賴 Go 子模組。
- 完成設定、IPv6 資源與目的政策 TDD：支援 `/3-/128`、正規化與範本重疊拒絕、1-4096 容量、全零主機位、CSPRNG 固定地址、canonical 引用、釘選池、刷新 draining、嚴格 IPv4 特殊範圍與 IPv6/ULA/NAT64 防繞過政策。
- YAML 設定採 schema version、KnownFields 嚴格解碼、可讀 duration、0600 同目錄暫存與 rename；預設值符合已確認契約。
- 此輪完整 `go test ./...` 與 `go vet ./...` 通過；Windows `go test -race` 因未啟用 CGO 無法執行，留待 Linux/CGO 驗收。
- 完成祕密與登入核心 TDD：0600 master key、AES-GCM 加密、CSPRNG 代理/管理憑證、Argon2id PHC 防資源濫用解析、單一 session、CSRF、每來源與全域滑動窗口限速；RED 均由缺少目標 API 引起，GREEN 後套件測試通過。
- 完成事件日誌與統計 TDD。第一輪 RED 為缺少 `eventlog.New/Event` 與 `stats.NewRegistry/Save/Load`；GREEN 實作 JSONL 檔案/stdout、欄位白名單、註冊祕密去敏、輪替、清除稽核、mutex-safe 即時/累計計數及原子快照。第二輪 RED 為缺少 `ResetAll`，GREEN 後可在保留 active 計數下歸零全部累計值。
- 此輪完整 `go test ./...` 與 `go vet ./...` 再次通過。`gopls` 未安裝，因此 LSP 診斷不可用；這是工具限制，後續仍以編譯、測試與 vet 驗證。
- 修復預設 resolver 契約偏差：RED 證明原先使用一般 DoT `::1111/::1001/::8888/::8844`，GREEN 改為 Cloudflare `::64/::6400` 與 Google `::6464/::64` DNS64 專用 IPv6 位址。
- 完成 `internal/dns64` TDD：resolver 端點順序故障轉移、raw DNS cache、正向 TTL 30 秒至 10 分鐘、負向 30 秒、每次 cache hit 重套目的政策、A 記錄 `/96` 本地合成、IPv4 literal、RFC 7050 類型探測與衝突來源、NAT64 立即/週期健康狀態。
- 完成 `miekg/dns v1.1.72` DoT adapter；依文件與原始碼使用 `tcp6-tls`、literal IPv6 目的、TLS server name、可選 IPv6 source。測試以注入 exchange 驗證 A/AAAA、TTL、RCODE 與傳輸錯誤，不依賴公網。
- DNS64 品質審查另發現公開 DNS64 可能先合成 `64:ff9b::/96`，導致自訂 prefix 無法使用；先建回歸測試，再允許有健康 NAT64 prefix 時從全數不可用 AAAA 繼續查 A 並以目前 prefix 重合成。手動 prefix 更新會先撤銷舊健康結果，重測成功後才可用。
- DNS64 階段完成後 `go test ./...`、`go vet ./...` 通過，健康排程測試連跑 20 次通過。
- 完成 `internal/network` 資源交易 TDD。第一輪 RED 為缺少 Kernel/Ownership/ResourceManager API；GREEN 後支援 address `/128`、local route + freebind、external 驗證、同批 DAD 平行等待與失敗回滾。第二輪 RED 補上 DAD 首錯取消、Reconcile/Release/Shutdown 與嚴格原子 YAML ownership store，GREEN 後可在啟動清理 stale owned 資源並修復 desired missing 資源，永不刪除 unowned 資源。
- 完成 Linux netlink adapter：逐址模式等待核心 DAD（不用 NODAD），local route 使用 local table/RTN_LOCAL，TCP6/UDP6 bind 驗證套 SO_BINDTODEVICE，route 模式另套 IPV6_FREEBIND；非 Linux constructor 明確拒絕。
- 完成 `internal/firewall` TDD：平台無關 manager 將管理 TCP、節點 TCP/UDP 與 relay 開孔正規化後交易式完整替換；Linux nftables backend 只管理 `inet s12ryt_ipv6`，單次 Flush 刪除/重建自有 table，絕不修改其他 table。診斷會保守列出其他 IPv4/IPv6/inet input base chain 的 policy-drop 與 drop verdict，但不自動覆蓋。
- crash-consistency 審查先以 RED 證明 kernel mutation 早於 ownership 持久化，GREEN 改為先保存 intent 再變更 kernel。另一輪 RED 證明 rollback 清理失敗會遺失 ownership，GREEN 改為保留所有未能移除的地址/route，讓下次啟動仍可安全對帳；intent 保存失敗則保證 kernel 零修改。
- 新增 `linux && integration` 的 root-only network namespace 測試，涵蓋真實 dummy link、address/DAD、local route/freebind、nftables 自有 table 建立與刪除。Windows 無法安全執行，但 integration test binaries 已在 Linux amd64/arm64 交叉編譯成功。
- 網路/防火牆階段完成後，`go test ./...`、`go vet ./...`、`GOOS=linux GOARCH=amd64/arm64 go build ./...` 均通過；真實 Linux root/netns integration 執行保留至部署環境驗收。
- 完成 `internal/proxy` TDD：mutex-safe round-robin IPv6 source lease 與刷新 draining、IPv6-only destination dialer、實際 TCP/UDP socket bind 的埠配置器、Linux SO_BINDTODEVICE/IPV6_FREEBIND system socket adapter。自動埠會逐一探測完整 transport 集合，wildcard/specific 衝突與失敗 rollback 均有測試。
- 完成 HTTP Proxy CONNECT 與 absolute-form HTTP 轉送；Basic 認證採 constant-time 比對，移除代理認證 header，錯誤不洩漏 URL path/header/content。完成 SOCKS5 method/auth、TCP CONNECT、自訂 UDP ASSOCIATE 與 BIND 明確拒絕；UDP relay 使用指定埠池、動態防火牆、來源驗證、FRAG 拒絕、每 `client+destination` 固定來源、雙向 idle 更新及控制連線關閉回收。
- 完成 mixed 同埠首位元分流、TCP tunnel 可選 idle timeout、逐節點 TCP/UDP 並行限制與協定 traffic metadata。各行為皆先由缺 API 或可重現缺陷形成 RED，再最小 GREEN。
- 完成 `internal/node` TDD：節點建立/編輯/啟停/刪除、無認證風險確認、專用池刪除、正式協定 handler builder、listener runtime 與連線終止。相同 endpoints 更新會原子替換 handler、不重新 bind，並立即關閉舊連線；不同 endpoints 先啟動 replacement 再停止舊 runtime。
- 新增防火牆協調器，交易式合併管理 TCP、各節點 TCP 與引用計數 UDP relay；舊 runtime 只可關閉與自身 endpoints 完全相符的世代，避免 replacement 規則被誤刪。backend 失敗不提交協調器狀態。
- 生命週期審查修復兩項缺陷：停止後自動埠重啟會保存實際配置 port；舊 runtime 清理失敗時保留已上線 replacement，更新管理狀態並回報 `ErrPreviousRuntimeCleanup`，不再反向關掉新服務。
- 代理/節點階段完成後，`go test ./... -count=1`、`go vet ./...`，以及明確設定 `GOOS=linux GOARCH=amd64/arm64 CGO_ENABLED=0` 的 `go build ./...` 均通過。
- 完成 `internal/admin` 管理安全邊界 TDD：Argon2id 密碼初始化/修改/重設與嚴格原子檔案、單一 session cookie、CSRF、Origin、JSON content type/body limit、來源與全域登入限速、僅暴露三態的公開 healthz，以及登入保護的去敏 SSE。
- 完成節點、IPv6 資源與操作 JSON API。所有 mutation 經 session/CSRF/Origin 保護並使用明確 DTO；內部錯誤回固定訊息。節點與資源變更只發布固定欄位 SSE，不攜帶帳密、session、CSRF 或任意 payload。
- 完成 IPv6 資源完整 State 驗證與原子 YAML 持久化，以及正式 `ResourceCoordinator`。每次 mutation 在候選 Store 執行，依序 network reconcile、state save、live commit；任一步失敗都對帳回舊狀態。強制 draining 終止依 terminate、network、state 順序執行。
- 完成 `OperationsCoordinator`：統計保存/歸零稽核、JSONL 尾端篩選、NAT64 runtime 更新與持久化失敗回滾、DoT resolver runtime 原子替換/cache清除與保存失敗回滾、健康聚合及連通性測試委派。設定新增可持久化的 canonical IPv6 `/96` `nat64_prefix`。
- 完成 Unix control protocol/client與 `admin reset-password [--data-dir PATH]`。運行服務透過 0600 socket即時重設並撤銷 session；control無法連線時，只有取得同一非阻塞 service flock 才能離線直接更新。Control runtime保證先鎖後listen、逆序清理；訊息讀取硬限制4KB且回應嚴格解碼。
- 管理階段完成後，`go test ./... -count=1`、`go vet ./...`、Linux amd64/arm64 `CGO_ENABLED=0 go build ./...` 與 admin Linux test binary交叉編譯均通過。Windows無法執行Linux flock/Unix socket及root網路整合測試，保留至Linux驗收。
- 完成繁中 React SPA TDD：同源 API client將CSRF只保留於記憶體，登入、登出、session恢復與SSE採固定schema；總覽、節點CRUD/啟停/帳密、IPv6範本/固定地址/池/draining、NAT64/DoT/連通性/管理密碼、metadata日誌與統計歸零皆有元件測試。
- 跨層審查修復刷新後CSRF遺失：已驗證的`GET /api/session`會輪替並回傳新CSRF，舊token立即失效；另補節點`authentication` DTO，使無認證與空白自動生成帳密可被明確區分。
- SPA品質審查修復登入401錯誤文案與768-1024px版面溢出。前端最終為7個test files、23項測試全綠，ESLint與Vite production build通過。
- Playwright以使用者提供的純Web開發頁搭配去敏API mock完成實際瀏覽器驗收：錯誤/正確登入、同源Origin、JSON與CSRF登出header、五個頁面、system/light/dark、375/768/1024/1025/1440皆無document/workspace水平溢出；桌面亮色與手機深色視覺檢查無遮擋，乾淨reload後console為0 errors。驗收截圖已刪除。
- 完成宣告式 IPv6 入站與 runtime 資源同步 TDD：節點只保存 `inbound_mode/inbound_resource`，固定地址或入站池解析為具體 listeners；pool刷新會先上新listener，保留未變listener，舊listener停止accept但既有連線無限排空；防火牆失敗時新generation完整rollback。管理API與SPA已同步改為具名資源選擇。
- 完成 SourcePool、出站與入站強制排空：來源lease可附著實際連線，force drain關閉指定舊來源的TCP/UDP mapping；多節點共用入站池由DrainTracker等待全部consumer完成，DrainQueue非阻塞回送ResourceCoordinator逐地址提交。自然與強制排空的stale callback安全no-op。
- 完成加密節點 YAML state、PersistentManager 交易保存/補償、desired running狀態關機保存及啟動restore。帳密只以AES-GCM欄位保存；節點CRUD state-save失敗會恢復runtime與設定，專用pool只在節點state成功提交後清理。
- 完成 production service 組裝：RunProduction先取得同一data-dir service flock；Service先預占雙棧management sockets，再初始化密碼、nft runtime、資源對帳與節點restore；背景執行HTTP/control、NAT64、drain queue、宿主位址週期刷新及30秒統計保存；正常停止依序nodes、network、firewall、stats、log。
- 完成正式 policy/outbound/inbound/node pipeline：動態掃描宿主local與全部managed IPv6；具名fixed/pool出站round-robin；IPv6-only DoT/dialer；SOCKS/HTTP/mixed handler；UDP relay與nft動態開孔；RuntimeResourceSynchronizer交易同步policy/outbound/inbound/listeners。
- 完成 production ConnectivityTester，管理頁會分別測DoT、原生IPv6、NAT64、每個fixed與非inbound pool代表來源；個別錯誤固定去敏且全部socket維持IPv6-only。
- 修復啟動期兩項高風險問題：draining consumer會先參考持久desired running nodes，避免restore前誤完成；損壞resources/nodes狀態不再於management bind前阻止Build。損壞resource store進入preflight只讀保護，任何mutation在kernel/runtime動作前拒絕且不覆寫原檔；node錯誤延後restore回報degraded。
- 修復崩潰殘留control socket：正式listener只移除Unix socket類型的stale路徑，regular file一律拒絕且保留。新增節點/更新帳密會即時註冊event logger去敏；宿主地址每60秒刷新，失敗保留舊政策並標degraded、成功恢復健康。
- CLI空參數與`serve --data-dir`已接正式production runner及SIGTERM context；首次管理密碼只stdout一次。root Go module透過local replace嵌入nested webui build產物。
- 新增Docker Node/Go多階段build、host network + NET_ADMIN Compose、systemd capability sandbox、安裝/移除腳本及繁中README。`.playwright-mcp/`已ignore且暫存檔清理。
- 最終驗證：`go test ./... -count=1 -timeout=300s`與`go vet ./...`通過；web 7 files/23 tests、ESLint、TypeScript/Vite build及web embed test通過；Linux amd64/arm64 binary build通過；network/firewall `linux && integration` test binaries兩架構交叉編譯通過；shell `bash -n`、Compose YAML及Dockerfile/systemd靜態契約通過。
- 未完整驗證：Windows主機沒有Docker CLI，未執行Docker build/compose runtime；沒有Linux root/network namespace，未執行真實netlink/nftables integration；`go test -race`因PATH無C compiler（`gcc` not found）無法啟動。這些是環境限制，不得誤報為通過。
- 使用者確認公開發布至 `https://github.com/s12ryt/s12ryt-ipv6`。本機初始化 `main`，依模組與測試契約建立 86 個原子提交；推送前常見私鑰/token格式掃描無命中，建置產物與Playwright暫存均由ignore規則排除。
- GitHub首次推送已由本機與GitHub API交叉確認：209個追蹤檔案完整存在，遠端`main`與本機HEAD同為`4198ded6fe3fb7a8a61caf8789ebb41c024797c4`，工作樹乾淨且upstream設定完成。
- 完成VPS一鍵安裝需求澄清並寫入`agent/question.md`第17節：GitHub Release latest/指定版本、Debian/Ubuntu與雙架構、SHA-256、交易升級/回滾、120秒健康檢查、首次密碼、active UFW開孔及明文HTTP風險。
- 以TDD新增`config get-management-port`與`config set-management-port`：CLI透過同一service flock及ConfigStore原子讀寫管理埠；無效port在任何mutation前拒絕，內部錯誤固定去敏。
- 以shell TDD新增根`install.sh`。RED依序涵蓋缺安裝器、交易健康失敗、既有服務停止失敗、固定安裝鎖、危險data path及備份失敗；GREEN後能偵測平台/架構、安裝依賴、解析latest或嚴格`vX.Y.Z`、HTTPS下載、精確checksum、交易安裝、健康失敗完整回滾、只對active UFW開孔、依本次啟動時間擷取首次密碼。
- 安裝器採全機固定`/run/lock/s12ryt-ipv6-installer.lock`，依賴加入`util-linux/flock`；既有unit停不下來或任一備份失敗時，在覆寫binary/unit/config前中止。`deploy/install.sh`改為重用根安裝器的相同交易核心。
- 新增GoReleaser v2與GitHub Actions release流程。手動dispatch先驗證嚴格語意版本、完成前端/Go/shell/GoReleaser檢查後才建立tag，並在同一次workflow run直接執行GoReleaser。既有tag只有在指向目前提交且尚無Release時才可安全續跑；發布時以`GORELEASER_CURRENT_TAG`避免同一提交多tag時選錯版本。Release同時提供Linux amd64/arm64裸binary、tar.gz與`checksums.txt`。
- Release工具驗證：Bash與Dash安裝/發布契約測試及語法檢查通過；GoReleaser v2.17.1 `check`與snapshot release成功，兩架構archive均包含binary、根installer、offline installer、unit、移除腳本及README，裸binary雜湊與checksums一致。ShellCheck在本機不可用。
- 修復workflow_dispatch只建立tag而未發布Release的缺陷：Actions run `30861047672`證明原`Publish GitHub Release`因push-only條件被跳過；先以`deploy/release_test.sh`形成RED，再移除該條件並加入tag提交/Release存在性檢查及明確current tag。修復提交已正常推送，未刪除或重寫既有`v0.1.0`、`v0.1.1`。
- GitHub Actions run `30861729476`成功建立並發布`v0.1.2`：`https://github.com/s12ryt/s12ryt-ipv6/releases/tag/v0.1.2`。前端、Go、shell、GoReleaser check、手動tag及Publish步驟全部成功，Release為latest、非draft、非prerelease。
- `v0.1.2`實際發布五個資產：`checksums.txt`、Linux x86-64/arm64裸binary與兩個tar.gz。重新下載後`sha256sum -c checksums.txt`四項全部通過；兩個archive均含README、根/離線安裝器、systemd unit、移除腳本與binary；`file`確認裸檔分別為靜態連結、stripped的x86-64與AArch64 ELF。驗證暫存檔已清理。
- 本輪完整回歸：Go所有packages測試與vet通過；React 7 files/23 tests、ESLint、Vite build及web embed test通過；Linux amd64/arm64 binary與network/firewall integration test binaries交叉編譯通過；GitHub Actions與GoReleaser YAML可由解析器讀取。真實VPS/systemd/UFW與Linux root netlink/nftables仍需在Linux VPS環境驗證。

## 2026-08-04

- 完成 Web 面板基礎／進階模式需求澄清並寫入 `agent/question.md` 第18節。模式預設為基礎，只保存於目前瀏覽器的 `s12ryt_panel_mode`，不寫後端；頂欄文字分段控制具 `aria-pressed`，切換不重建頁面或清除未送出表單。
- 先以五個元件測試檔建立 RED：驗證模式預設與持久化、切換無 mutation、節點隱藏值保留、資源安全預設，以及網路／日誌破壞性或進階控制的漸進揭露。GREEN 後基礎模式保留五頁與日常操作，進階模式維持原完整介面。
- 基礎節點表單只隱藏 limits、timeouts 與 ULA；既有值提交時原樣保存。基礎資源表單預設 `address`、自動固定地址及 10／100／15 池容量；網路保留診斷、連通性與密碼，resolver 唯讀；日誌保留全部查詢條件並隱藏清除／歸零。
- 品質審查另以 RED 重現基礎日誌統計仍顯示空白「操作」欄，GREEN 後桌面表格改為實際六欄，進階模式仍保留第七欄操作。
- README 新增管理面板模式、五頁操作對照、代理／資源、三種 Linux IPv6 配置、DNS64／NAT64／DoT／DAD／ULA／freebind／wildcard／draining 與健康狀態詞彙表及建議操作順序。
- 最終驗證：前端 7 files／28 tests、ESLint、TypeScript 與 Vite production build通過；Go全套測試、vet及web embed test通過。Playwright以去敏API mock驗證基礎／進階模式、表單狀態、localStorage與零mutation，並在375／768／1024／1440寬度逐一檢查五頁，40組均無document/workspace溢出或頂欄重疊；1440亮色與375深色視覺檢查無截斷，乾淨後續console無新增錯誤。
- 完成網路候選與自動命名需求澄清並寫入 `agent/question.md` 第19節。Linux偵測只納入 UP、非 loopback 介面，以及位於 `2000::/3` 的原生全球 IPv6 地址／核心路由前綴；瀏覽器只透過登入保護 API 取得候選，不自行推測主機狀態。
- 以TDD新增 `internal/network` discovery與Linux netlink adapter。相同介面／前綴會合併address、route來源，候選穩定排序；任一核心查詢錯誤時不回傳部分結果。`internal/admin`候選服務會將與既有範本相同、重疊或包含的候選標記為不可用，API錯誤固定去敏。
- production builder已註冊登入保護的 `GET /api/discovery/network`；建立builder不會立即讀netlink，只有管理員請求候選時才偵測。Windows完成核心單測與Linux雙架構交叉編譯，未在真實Linux VPS執行netlink discovery。
- 前端新增節點與資源自動命名、IPv6資源頁介面／CIDR候選與自訂備援。候選載入或刷新失敗時保留舊結果；衝突候選仍顯示原因但不可選。品質審查以RED重現表單先開啟、候選稍後抵達的時序問題，GREEN後僅未被管理員修改的表單會自動採用候選；明確切回自訂後不再被覆寫。
- 進階網路頁將NAT64改為自動探索／自訂 `/96` 模式，並提供Cloudflare與Google四個DNS64端點預設；相同address+port的端點不可重複加入，自訂Resolver仍保留。基礎模式維持唯讀。
- 本輪最終驗證：Go全套測試與vet、Linux amd64/arm64 build、network/firewall integration test binary交叉編譯、web embed test通過；前端8 files／34 tests、ESLint、TypeScript與Vite production build通過。Playwright以去敏API mock驗證候選衝突、介面切換、自動命名、NAT64模式與Resolver預設，並完成375／768／1024／1440、基礎／進階與五頁共40組無水平溢出檢查；console無新增錯誤。
- 修復公網HTTP管理面節點複製失敗：原元件直接呼叫可能不存在的 `navigator.clipboard.writeText`，在不安全context會同步拋出TypeError。先以純函式與元件測試形成RED，再新增Clipboard API→隱藏textarea/`execCommand`→手動可選取對話框的三級降級；成功後於節點操作旁顯示2秒「已複製」。
- 節點新增獨立「複製連線資訊」與「複製連線帳密」。連線資訊依SOCKS5／HTTP／mixed輸出逐行標準URI，百分比編碼userinfo、IPv6加方括號；IPv4使用面板hostname，固定IPv6使用具名地址，入站池每次隨機一個active地址，雙棧輸出並去重。空池、錯誤類型、缺失／歧義資源與不完整帳密均在複製前拒絕。
- 複製修復驗證：前端10 files／43 tests、ESLint、TypeScript與Vite production build通過，LSP無診斷。Playwright在實際HTTP頁停用Clipboard API，驗證相容備援成功、2秒回饋消失、全自動失敗時對話框自動全選正確mixed IPv6 URI；375／768／1440操作列與頁面無水平溢出，console為0 errors。
- 完成一鍵批次建立節點與資料夾需求澄清並寫入 `agent/question.md` 第21節。批次使用共用設定與逐列預覽，預設5、上限100；後端為每個credentials節點獨立生成安全帳密，無認證批次只需整批確認一次公開代理風險。
- 以TDD擴充節點模型與加密狀態：`folder`會trim、限制64個Unicode字元並拒絕控制字元；舊狀態缺欄位時載入為未分類。移動與改名只修改metadata，不重啟runtime；持久化失敗會回復原資料夾。
- 批次建立採完整preflight、逐一啟動、單次state保存與反向rollback。無效設定、批內ID／名稱／手動埠衝突、既有資料夾衝突都在任何runtime啟動前拒絕；啟動或保存失敗時依反序停止已啟動節點。另以回歸測試拒絕批次API手填帳密，確保每筆credential只能由後端CSPRNG產生。
- 管理API新增批次建立、節點移動、資料夾改名，以及保留成功並逐項固定去敏回報失敗的整批啟停／刪除操作。資料夾刪除明確不是跨節點原子交易，成功項不因其他節點失敗而回復。
- React節點頁新增「一鍵建立多節點」、共用設定、ID／名稱／埠預覽、資料夾排序／收合與批量複製／啟停／刪除。收合狀態只保存在 `s12ryt_node_folders_collapsed`；未分類始終最後且不提供伺服器資料夾操作。基礎模式隱藏進階限制，模式切換不清除批次表單或預覽。
- 品質審查以Playwright發現未分類名稱空字串會誤觸資料夾刪除確認，先新增RED測試，再要求非空folder才能顯示確認。最終前端11 files／53 tests、ESLint與Vite build通過；Go全套測試、vet、Linux amd64／arm64 build與network/firewall integration test binary交叉編譯均通過。
- Playwright使用去敏API mock實際驗證批次建立、資料夾收合／移動／改名、部分失敗的整批停止與未分類安全行為；375／768／1024／1440在基礎／進階模式下皆無document、main、批次預覽或資料夾標題水平溢出，console為0 errors。Windows仍未執行真實Linux root/netns網路整合測試。
- 完成管理面板 modal、控制項、側欄與動畫需求澄清，契約記於 `agent/question.md` 第22節。登入頁維持專用畫面；登入後所有設定填寫與寫入命令改由 modal 承載，連通性測試及日誌篩選仍可直接執行。
- 以TDD新增共用 `ModalDialog`：React portal、ARIA dialog/modal/title、focus trap與觸發點還原、body背景捲動鎖、backdrop不可關閉；乾淨表單的Esc／X／取消可直接關閉，髒表單會疊加「放棄未儲存的變更」確認。品質審查再以RED證明巢狀確認期間底層dialog仍可能被輔助技術讀取，GREEN後套用`aria-hidden`及`inert`，第二層Esc只返回原編輯表單。
- 以TDD新增保留native input與鍵盤語意的 `CheckboxField`，並統一text、number、password、textarea、select、checkbox的亮／暗主題、focus、disabled與hover樣式。桌面側欄可由220px收合至68px，偏好保存於`s12ryt_sidebar_collapsed`；手機仍使用橫向導覽。
- 節點單筆與批次、資料夾、IPv6資源、NAT64、Resolver、管理密碼、統計歸零及日誌清除等寫入操作皆遷移至modal。批次節點採共用設定、逐列預覽、最終確認三步；返回及基礎／進階切換均保留表單與預覽。所有modal具固定header/footer及獨立捲動body，手機使用近全屏版面。
- CSS加入160至220ms的modal/backdrop、頁面、側欄、收合、步驟與回饋動畫，沒有hover scale或layout shift；`prefers-reduced-motion`下計算後動畫與transition降至0.01ms。
- 本輪驗證：前端13個test files／63項測試、ESLint、TypeScript與Vite production build全部通過，LSP對核心modal與日誌元件無診斷。Playwright以去敏API mock驗證側欄偏好、focus trap、髒表單巢狀確認、鍵盤Esc返回、375px手機全屏modal，以及桌面代表性wide／medium／confirm modal；五頁在375px無document、workspace或警告列水平溢出，console為0 errors。瀏覽器、Vite程序、截圖與臨時檔均已清理。

## 2026-08-05

- 檢查本機 `origin` 與 GitHub 遠端 `s12ryt/s12ryt-ipv6`：倉庫原先沒有 `LICENSE`／`COPYING`、README 授權章節、SPDX 中繼資料或其他授權聲明，因此公開可見但尚未授予開源使用權。
- 使用者確認自本次 `main` 與後續版本採 `AGPL-3.0-or-later`，著作權聲明為不列年份的 `Copyright (C) s12ryt`；不加入逐檔標頭、不修改或重發既有 `v0.1.2` Release，第三方相依套件維持各自授權。
- TDD RED：擴充 `deploy/release_test.sh` 後，測試因缺少根 `LICENSE` 如預期失敗並輸出 `FAIL: GNU AGPL license file is missing`。GREEN：新增 GNU AGPL v3 標準全文、README 授權與網路服務原始碼義務說明、`web/package.json`／lockfile 根套件 SPDX，並將 `LICENSE` 加入 GoReleaser archive files；同一契約測試通過。
- 完整驗證：前端 13 個 test files／63 項測試、ESLint、TypeScript與Vite production build、web embed test、Go全部套件測試與 `go vet`、shell語法、installer/release契約測試均通過。系統未安裝獨立 `goreleaser`，改以 `go run github.com/goreleaser/goreleaser/v2@v2.17.1 check` 驗證，使用自動下載的 Go 1.26.5 toolchain確認1份組態有效。
- 品質審查確認 LICENSE 含完整第0至17條、第13條遠端網路互動義務、第14條後續版本選項、免責與附錄；前端build沒有造成額外追蹤檔變更。未執行既有Release重發或真實GitHub Actions發布，符合已確認範圍。
- 授權核心、Web SPDX、Release封裝與治理紀錄分別提交為 `79bd31e`、`d611785`、`6b3f8e1`、`60f10ff`；透過現有 GitHub CLI 憑證的一次性 WSL credential helper 一般推送至 `origin/main`，未寫入 Git 設定。推送後本機 `HEAD`、`origin/main` 與 GitHub 遠端均為 `60f10ff8357cb3aabd2ace3cd3a31657c91e2a7e`，工作樹乾淨。

## 2026-08-24

- 接手時確認本機 `main`、`origin/main` 與 GitHub 遠端均為 `76a1d19`，工作樹乾淨；Go全套測試、vet、前端63項測試/lint/build、web embed、installer/release shell測試與Linux amd64/arm64交叉build基線均通過。Windows環境仍無法執行真實Linux root/netns integration、Docker、race detector與本機GoReleaser。
- 完成本機 Agent CLI 需求澄清並記於 `agent/question.md` 第24節。入口固定為 `s12ryt-ipv6 agent ...`，透過root擁有的0600 Unix control socket呼叫正式Resource/Node/Operations/Config服務，不直接改YAML、不經明文HTTP；一般stdout為單一JSON，schema/export成功直接輸出文件。
- 以TDD將control protocol由4 KiB擴至4 MiB，新增泛用agent RPC、嚴格單JSON/unknown field拒絕、transport與business envelope分層、permission cause保留、requested timeout與30分鐘server cap。品質審查另修復apply預設10分鐘仍會被server 30秒截斷，以及Serve取消無法中止進行中handler的關機延遲。
- 以TDD新增完整Draft 2020-12 strict schema、JSON/YAML export/apply、settings欄位級合併、active/configured與restart_required、遮罩帳密round-trip，以及resources/nodes/network/logs/stats完整命令樹。所有刪除、prune、force-drain、清除日誌與重設統計均要求非互動`--yes`。
- Apply採全文預檢後依序執行，失敗回報已完成項目且保留各正式service既有rollback。穩定性回歸修復跨領域node/resource錯誤引用、prune刪除保留node依賴、prune資源圖借用即將刪除依賴、existing pool default/pinned錯判，以及dry-run `authentication.generate`仍消耗entropy；credential generator失敗會在任何設定或runtime mutation前停止且不洩漏底層錯誤。
- CLI支援完整樹狀命令、JSON stdin/file、apply/export明示JSON或YAML、4 MiB限制、單文件嚴格解析、1秒至30分鐘timeout、固定錯誤碼與退出碼、`--show-secrets`及health非healthy退出1。回歸另修復未公開命令被誤接受、無參命令忽略arguments、混合批次無認證確認與YAML數值型別。
- Production builder將AgentService接入control socket；一鍵與離線安裝器在HTTP health後驗證Agent status，結構有效的degraded仍成功，socket/權限/逾時/無效JSON/錯誤envelope會完整回滾。Agent health shell parser拒絕prefix/trailing/error偽成功，成功後輸出status/schema/JSON與YAML export及安全dry-run quickstart。
- 本輪RED證據包含缺少AgentService/ConfigStore.Replace/control API、unsupported命令、缺CLI route/parser、production control不支援agent、server timeout契約缺失、installer缺agent gate/quickstart，以及穩定性缺陷的可重現測試；各項均由最小GREEN與鄰近回歸保護。
- 最終驗證全部通過：`go test ./... -count=1 -timeout=300s`、`go vet ./...`、前端13個test files／63項測試、ESLint、Vite 8.2.0 production build、web embed test、shell語法、installer與release契約測試。另以`CGO_ENABLED=0`成功交叉建置Linux amd64／arm64主程式，並編譯兩架構的network/firewall integration test binaries；`go version -m`確認GOOS/GOARCH、revision `76a1d19fd3665da92f83c662e05c572de510896c`與工作樹尚未提交所預期的`vcs.modified=true`。
- Windows環境未執行需要root、disposable network namespace與nftables的真實Linux integration runtime；本機也無Docker、獨立GoReleaser與GCC，因此未執行容器建置、GoReleaser發布流程與race detector。交叉編譯僅證明兩架構可編譯，不取代Linux實機網路行為驗證。

## 2026-08-25

- 接手確認：HEAD `76a1d19` 與 `origin/main` 一致，但 8/24 的 Agent CLI 全部變更（16 個修改 + 8 個新檔）尚未提交，工作樹為完成待收尾狀態。
- 重建基線全部通過：`go test ./... -count=1 -timeout=300s`（15 packages）、`go vet ./...`、前端 13 files／63 tests、ESLint、Vite production build、`deploy/install_test.sh`、`deploy/release_test.sh`、根與 deploy shell 語法檢查，以及 `CGO_ENABLED=0` Linux amd64/arm64 交叉建置。
- 發現 `NodesView` 的 SSE `waitFor` 測試在與 Go build/vet 並行搶 CPU 時會暫態超時（首次 49s、單獨重跑 12s 全綠）；之後執行前端測試應避免與重編譯並行，避免誤判為缺陷。
- 依使用者指示以 7 筆依賴有序原子提交收尾 8/24 工作（control 擴充、config replace、agent service、production 接線、agent CLI、安裝器 agent gate、文件與治理紀錄）並推送 `origin/main`。
- 提交期間發現 Windows `core.autocrlf=true` 且無 `.gitattributes` 的陷阱：stash/checkout 流程會把工作樹文字檔 smudge 成 CRLF，WSL shell 測試隨即失敗（`set: Illegal option -`）；repo 內提交內容經 `git cat-file` 驗證仍為純 LF 未受污染。已以 HEAD blob bytes 直寫恢復工作樹並重驗 shell 契約測試與 Go 全套回歸。教訓：此環境下避免依賴 stash 驗證中間提交；長期應考慮加入 `.gitattributes` 固定 LF（未經使用者同意，本次未動）。
- 使用者長期指示：其對網路領域理解有限，後續需求澄清**不得追問細緻的網路技術細節**。網路參數與技術選型由 Agent 依既有慣例、安全預設、風險與可維護性自行決定並以白話簡述取捨；提問只聚焦使用者想要的結果與行為；僅高風險或不可逆決定需以白話標明後果徵求同意。
- 完成 Web UI 視覺美化自主疊代（契約見 `agent/question.md` 第25節）：定案「Teal Console」深青基礎設施主控台風格，建立 canvas/chrome/surface 三層視覺深度、品牌 teal 漸層方塊、狀態膠囊圓點、桌面 nav 內縮 teal 指標條（手機改頂部）、卡片化 metrics/資料區/資料夾/日誌篩選、tabular-nums、登入頁 radial 光暈加細網格背景、button:active 1px 下沉回饋與 dark 主題 `--on-primary` 深色按鈕文字。系統字型堆疊加入 Noto Sans TC 優先序，不載入外部資源；新增內嵌 SVG teal 鎖形 favicon 消除每次載入的 favicon 404。
- 本輪 TDD：先在 `styles.test.ts` 追加 10 項視覺刷新契約形成 RED（全數因目標樣式不存在而失敗、5 項舊契約保持通過），GREEN 後 15/15 通過。既有響應式/modal/動畫契約字串全數原樣保留。
- Playwright 實機驗證：以 init-script 去敏 API mock（session/overview/nodes/resources/stats/discovery/logs + no-op EventSource）在 Vite dev server 執行，4 寬度×2 主題×5 頁面共 40 組 document/workspace 零水平溢出；favicon 修復後 console 歸零；computed style 逐項證實 nav rail `rgb(15,118,110) 3px inset`、膠囊 6px 圓點 color-mix 底、卡片 14px 圓角加陰影、chrome `rgb(243,246,249)` 與 surface 分離、dark `--on-primary #04211d`、tabular-nums 生效。`look_at` 視覺代理兩次無回應，改以 computed style 客觀證據替代；驗收截圖存於 `.playwright-mcp/verify-*.png`（已 ignore）。
- 本輪完整回歸：前端 13 files／73 tests、ESLint、TypeScript/Vite build、`go test ./... -count=1`、`go vet ./...` 與 Linux amd64/arm64 `CGO_ENABLED=0` 交叉建置全部通過。
- 應使用者要求修正 SSH 連入後的 IPv6 洗版（契約見 `agent/question.md` 第26節）：根源為 `internal/eventlog` 每筆事件（含每次代理連線的 source/destination/outbound IPv6）鏡射 stdout→journal。改為 `proxy` 類事件只寫 JSONL 檔案、不輸出 stdout/journal；`system`／`audit` 事件維持 stdout/journal 輸出。Web UI 日誌頁、`/api/logs` 與 `agent logs tail` 查詢行為不變。
- TDD：先改寫 eventlog 測試為新契約（proxy 事件不得出現於 stdout、system/audit 仍鏡射、檔案保留全部四筆事件），RED 重現 proxy 事件洗版 stdout；GREEN 後 eventlog/app/admin/node/cmd 全綠。README「健康與日誌」段同步更新；完整 `go test ./...`、vet 與雙架構交叉建置通過。
- 使用者澄清「一登入 SSH 就有當前 IP 列表」與 journal 無關。全面排查證據：install.sh/deploy 腳本零 motd/profile.d 寫入；systemd unit 無 console 輸出；整個程式 stdout 僅 service.go 的首次管理員密碼一行。診斷：登入時的 IPv6 牆來自 VPS 發行版/供應商登入訊息列出網卡位址，而 `address` 模式本就會把代理 IPv6 掛上網卡。
- 使用者後續偏好：**要保留登入歡迎訊息，只隱藏其中 IPv6**。README「SSH 登入時顯示大量 IPv6」一節已改為過濾方案：先以唯讀 grep 命令找出列 IP 腳本（涵蓋 landscape-sysinfo／hostname -I／ifconfig／ip addr／ip -6），專列 IP 的供應商腳本直接 `chmod -x` 停用；Ubuntu `50-landscape-sysinfo` 因混合負載/記憶體資訊改用 wrapper 過濾（備份 `.50-landscape-sysinfo.orig` 含點檔名不會被 run-parts 執行，可冪等重跑與一鍵還原）。兩個命令皆已於 WSL 模擬環境實測：過濾後保留系統資訊/IPv4/記憶體、三行 IPv6 全消失；冪等與還原路徑驗證通過。程式碼零變更（純文件，TDD 例外）。

## 2026-08-25（第二輪）：穩定性審查與崩潰修復

- 使用者要求全面穩定性審查並回報 VPS「過沒多久就崩潰」。掃描結論：goroutine 產生點 8 處生命週期全部健全（watcher 皆有 done channel、copy 通道有緩衝、DAD 有 WaitGroup＋逾時）；管理 HTTP 有 `ReadHeaderTimeout`＋`IdleTimeout`（SSE 依賴無 WriteTimeout，合理）；`internal/auth` LoginLimiter 每次 Allow/RecordFailure 都 prune，SSE broker 有 mutex 清理，皆無洩漏。
- 確認缺陷 A（高）：`internal/dns64` resolver 的快取 map 無上限也無清理——代理拜訪愈多獨特網域記憶體無限成長（長時間運行的慢性 OOM 因素）。以 TDD 修復：新增 `cacheMaxEntries`（預設 4096）與 `evictLocked`（先清過期、仍超限淘汰最早到期）；RED 兩測因欄位不存在失敗，GREEN 後 dns64 全綠。
- 確認缺陷 B（高，最可能的崩潰元兇）：`internal/node` runtime 每條代理連線一個 goroutine 且 `serve()` **無 panic recover**——公網掃描亂封包使 SOCKS5/HTTP/mixed 解析 panic 時整個程序崩潰（管理 HTTP 由 net/http 內建 recover 保護，代理資料路徑沒有）。以 TDD 修復：`dispatch()` 以 recover 將 panic 轉為單連線錯誤 `proxy handler panicked: %v`；RED 測試 `TestListenerRuntimeSurvivesHandlerPanic` 修復前整個測試 binary 被 panic 炸掉（stack trace 正是 `runtime.go:266 ServeConn` ← accept goroutine），GREEN 後程序存活、發出含 panicked 的 TrafficTCPClosed 事件、後續連線繼續服務。
- 已知有界殘留（列建議未修）：刪除節點後 stats registry 殘留 entry（節點上限 1024、極小）；eventlog `RegisterSecret` 隨節點建立/輪換緩慢成長（量小）。皆非崩潰因素。
- 完整回歸：`go test ./... -count=1`（15 packages 含新測試）、`go vet`、Linux amd64/arm64 `CGO_ENABLED=0` 交叉建置全部通過。
- 使用者要求「再仔細翻翻」之第二輪深查（fd 洩漏／崩潰殘餘）：UDP relay mapping 生命週期（idle deadline 清理＋association 結束 closeMappings 全關）、DoT 每查詢連線由 `ExchangeContext` 自動關閉、port allocator 探測/預約 socket 全路徑關閉、network netlink 採 package-level 共享介面——全部排除洩漏。
- firewall nftables 虛警澄清（重要教訓）：曾以 RED 測試（WSL 執行交叉編譯的 Linux test binary，實測 closed=0）懷疑 `nftables.New()` 每交易洩漏 netlink fd；查 module source（nftables v0.3.0 conn.go L68、L138-147、L256-260）後確認 **transient 模式下 `New()` 不開 socket，每操作由庫臨時 dial 並 `defer closer()` 自動關閉**，僅 `AsLasting()` 需要 `CloseLasting`。backend 無洩漏，測試已撤銷並以 WSL 實跑全套 PASS 驗證還原。
- 剩餘低暴露缺口列建議未修：control socket agent 指令處理無 panic 防護（僅本機 root 可達 0600 socket，暴露度低）。崩潰最可能成因維持先前已修兩項：代理連線 panic（已隔離）與 dns64 快取無限成長（已上限）。

## 2026-08-25（第三輪）：角落深查（協定逾時／持久化／SSE／池）

- 觸發：使用者「翻翻那些你可能不會在意或沒看到的」。鎖定前兩輪未覆蓋面：協定層時間邊界、SSE 慢消費者、共享物件併發、持久化原子性、前端 API 行為。期間依使用者指示完成上下文壓縮（180 則訊息→6 段摘要）。
- 正面確認（零變更）：三協定握手逾時正確套用與清除；HTTP `authorized()` constant-time 比較、`httpProxyTarget` 限 absolute-form http 且拒 userinfo；`relayConnections` 的 `tunnelDeadlineRefresher` 以 mutex 刷新雙向 deadline、`stopWatch` channel 正確收尾、`CloseWrite` half-close；SSE `validateEvent` 擋 `\r\n`（防回應注入）+ `json.Marshal` 轉義 + `Publish` drop-oldest 有界佇列；`SourcePool` Acquire 只選 current、Replace 未活動地址即時回收/活動中進 draining、ForceDrain 鎖外逐一 force、Release `once` 防重複、Attach 失敗路徑全關 closer；正式狀態檔全部 CreateTemp→Chmod 0600→Write→Sync→Close→Rename 原子寫（config.go L161-197 為範本），vault `O_EXCL` 一次建檔；web `api.ts` 無自動重試循環、subscribe close 冪等；`main.go` `signal.NotifyContext` 貫穿 RunProduction。
- 新觀察 A（中低，列建議未修）：eventlog `Tail` 與 `Write` 共用 mutex，Tail 持鎖解碼最多 5×100MB 期間，所有代理連線關閉的 proxy 事件寫入（TrafficObserver 在連線 goroutine 內同步呼叫 Write）排隊——管理頁查日誌時「關閉中連線」延遲 spike；不影響新連線接受、無洩漏無崩潰、單人管理情境查詢頻率極低。若未來要修：Tail 改為 snapshot 檔案集合後無鎖掃描（弱化跨檔一致性保證），需先建立穩定的併發量測測試（Mutex 無外部持鎖觀測點，直接斷言互斥順序不穩定）。
- 新觀察 B（低，列建議未修）：`rotateLocked` 若 rename 鏈完成後 reopen 失敗（磁碟滿/IO 錯誤），`l.file` 保持已 Close handle，後續每筆 Write 持續報錯直到重啟；錯誤經 report 回呼隔離、file 從不設 nil 故無 panic 風險，且會被代理 dispatch recover 與 net/http 內建 recover 兜住。修復需注入檔案系統錯誤的測試介面（Logger 直用 os 包），改造成本高於收益。
- 本輪零程式碼變更；`go test`/工作樹無異動，僅治理紀錄更新與提交。

## 2026-08-25（第四輪）：IPv6 池新建路徑審查（bug 排除＋瓶頸 B1 修復）

- 觸發：使用者「看看新建 ipv6 池那部份的代碼有沒有 bug 或瓶頸」。審查範圍：`internal/ipv6resource`（store/template/random/state/state_store）、`internal/admin/resource_service.go`（ResourceCoordinator 事務：clone→mutate→network.Reconcile→runtime.Sync→Save→swap，含三層回滾與 rollback 用 `WithoutCancel`）、`internal/network`（manager/kernel_linux/ownership_store）、production 接線（dadTimeout=60s、TimeoutStopSec=90s）。
- 正確性結論（無 bug）：CreatePool/RefreshPool/DeletePool/CompleteDrain* 引用計數與 `buildStoreFromState` 交叉驗證一致（fixed+active非pinned+draining 各計一次 = References）；drain 批次 ID 單調且 NextBatch ≥ 最高批；address 模式衝突檢查（exists&&!owned 拒絕）；Apply 先存意圖再動 kernel；事務任一步失敗回滾並自癒（owned-but-missing 地址下次 reconcile 清理或重加）；GenerateAddresses 循序掃描有界（prefix 大小）；`capacity==len(pinned)` 的 err-reset 寫法脆弱但語義正確；池地址循序分配（first-fit）屬設計選擇非缺陷（固定地址才用 crypto/rand 隨機）。
- 瓶頸 B1（中高，已修）：`removeStale`/`releaseAddresses`/`releaseRoutes` 每移除一個地址就 `FileOwnershipStore.Save`（YAML marshal O(n)＋CreateTemp＋Chmod＋Write＋**fsync**＋Rename）——刪 C 地址池 = C 次 fsync＋O(C²) 寫入量；C=4096 估 4–40 秒且服務停止（Shutdown→removeStale）同路徑，逼近 systemd 90s；全程持 coordinator＋ResourceManager 雙鎖阻塞所有資源 API。TDD 修復：改為迴圈後單次批次保存（stateChanged 旗標；部分失敗時仍保存成功移除的部分狀態；crash 視窗內 ownership 檔可能殘留已移除條目 → 下次 reconcile 自癒，與原語義等級相同）。RED：`TestReconcileBatchesOwnershipSavesWhenRemovingStaleAddresses`（saves=5）與 `TestReleaseBatchesOwnershipSaves`（saves=3）失敗；GREEN：saves≤2/≤1；`TestReconcilePersistsSuccessfulRemovalsWhenARemovalFails` 守護部分失敗語義全程通過。
- 瓶頸 B2（中，列建議）：`linuxKernel.AddressExists` 每次呼叫 = LinkByName（RTM_GETLINK 往返）＋`AddrList` 全量 dump O(介面地址數)——applyAddresses 衝突預檢與 removeStale 對 C 地址各做一次 → O(C²) 解析。修復需 Kernel 介面新增批次查詢（一次 dump 對全部 refs），屬結構性重構；C≤100 時影響亞秒。
- 瓶頸 B3（中低，列建議）：`waitForDAD` C 個平行 goroutine 各自每 100ms 全量 `AddrList` dump——DAD 窗口（約 1s）內 O(C²/s) CPU 尖峰；C=4096 時可達億級解析/秒。修復需共享單一輪詢器批次檢查全部 pending refs。
- 瓶頸 B4（低，列建議）：ResourceCoordinator 單一 mutex 涵蓋整個事務（含 netlink＋DAD 最長 60s＋fsync），`Snapshot()` 讀取也走 Lock——大池操作期間資源頁/agent 資源命令阻塞；代理資料路徑（PolicyProvider 自有 RWMutex）不受影響。
- 規模註記：基礎模式預設容量 10/100/15 時上述瓶頸全部亞秒；僅進階模式大容量（數百～4096）才顯著。B1 修復後刪池/停機的 fsync 數從 O(C) 降為 O(1)。
- 回歸：`go test ./...`（15 packages）、`go vet`、Linux amd64/arm64 交叉建置全過；治理紀錄更新並提交。

## 2026-08-25 第五輪操作記錄（池輪換審查＋R1/R2 修復）

- 讀取：internal/app/drain_queue.go、drain_queue_test.go、internal/admin/resource_service.go（CompleteDrainedAddress/ForceDrain/transact/commitCandidate/drainingBatch/isDrainingAddress）、resource_service_test.go（helper：memoryResourceStateStore/fakeResourceNetwork/fakeResourceRuntime/fakeDrainTerminator/resourceStateWithPool）、internal/ipv6resource/store.go（CompleteDrain/CompleteDrainedAddress）、internal/app/production_build.go（NewDrainQueue 接線 L251、ReconcileResources 閉包 L432）、agent/question.md。
- 修改：internal/app/drain_queue.go（介面改 CompleteDrainedAddresses 批次簽名、Run 按 pool 分組保序、groupDrainedAddressesByPool）；internal/app/drain_queue_test.go（recordingDrainCompleter 改批次、新增 TestDrainQueueGroupsCompletionsByPool、既有兩測試適配、waitForDrainAddresses）；internal/admin/resource_service.go（新增 CompleteAllDrains 與 CompleteDrainedAddresses、CompleteDrainedAddress 改委託）；internal/admin/resource_service_test.go（新增 5 測試：批次單事務、skip finished+去重、nothing draining no-op、無效輸入、CompleteAllDrains 清殘×2）；internal/app/production_build.go（ReconcileResources 先 CompleteAllDrains 再 Reconcile）；agent/question.md（§27）、agent/deep_todos.md、agent/memory.md。
- 驗證指令：go test ./internal/app ./internal/admin（RED 編譯失敗→GREEN）；go build ./... && go vet ./... && go test ./... -count=1（15 pkgs 全綠）；CGO_ENABLED=0 GOOS=linux GOARCH=amd64/arm64 go build（雙架構 OK）。
- 提交：單一 fix 提交（R1+R2 同檔耦合）＋docs 提交，推送 origin/main。

## 2026-08-25 第六輪操作記錄（F1/F2 修復）

- 讀取：internal/firewall/manager.go、manager_test.go、backend_linux.go、backend_linux_test.go、internal/node/firewall_coordinator.go、firewall_coordinator_test.go、internal/app/production_build.go（L410-439）、internal/config/config.go（PortRange 預設）、internal/app/policy_provider.go、policy_provider_test.go、internal/policy/destination.go、agent/question.md。
- 修改：manager.go（Opening.PortEnd＋normalize 驗證/dedup/排序）；backend_linux.go（範圍 Gte/Lte）；firewall_coordinator.go（relayScope 計數＋relayPortMin/Max＋建構子 5 參數）；production_build.go（接線 settings.Ports.Min/Max）；policy_provider.go（Policy() 免 clone＋文檔、刪 cloneAddressSet）；destination.go（DestinationPolicy 唯讀文檔）；manager_test.go（+3 測試）；firewall_coordinator_test.go（全檔改寫）；backend_linux_test.go（範圍表達式測試）；policy_provider_test.go（imports reflect/sync/time＋mutation 斷言改寫＋3 新測試＋managedAddressState helper）；agent/question.md（§28）、deep_todos.md、memory.md。
- 驗證記錄：F1 RED=go test 編譯失敗（PortEnd/參數數）＋GOOS=linux test -c 同因；GREEN=firewall/node/app 三套件綠＋WSL firewall binary PASS。F2 RED=TestPolicyProviderPolicyReturnsZeroCopyViews "copied the local address set"；GREEN=6/6 測試綠。go vet OK；go test ./... 15 packages 全綠；CGO_ENABLED=0 GOOS=linux GOARCH=amd64/arm64 build OK。-race 不可執行（無 gcc，記錄於 §28.2）。
- 提交：F1 perf(firewall)＋F2 perf(app) 與 docs，推送 origin/main。

## 2026-08-28 第七輪操作記錄（後端核心與長連線修復）

- 契約：完成三輪澄清，將全後端核心範圍、長連線逾時/half-close/UDP 雙向活動語意、相容性與驗收門檻寫入 `agent/question.md` §29；本輪不新增 schema 或公開 API。
- UDP RED：`TestUDPAssociationRemoteTrafficRefreshesAssociationIdleTimeout` 因第二次遠端回應時 pipe 已被 association 固定逾時關閉；`TestUDPAssociationRemovesMappingWhenClientWriteFails` 因一秒後 stale mapping 仍存在；`TestUDPAssociationReportsPacketDeadlineFailure` 因原始 deadline 錯誤被吞，僅回 closed-network 錯誤。GREEN：`udp_relay.go` 加入 association deadline 刷新、async error 傳遞與 WriteTo 失敗移除 mapping；proxy 全套通過。
- Node RED：`TestListenerRuntimeMetadataUpdatePreservesActiveConnections` 先因 rename 更新重建 handler 失敗；加入 no-op 情境後再次因 builder calls=2 失敗。GREEN：`ListenerRuntimeFactory.Replace` 對 runtime 行為等價設定只更新 config，保留 handler/active connections；真正行為設定變更測試改以 HandshakeTimeout 變更守護重建語意；node 全套通過。
- Eventlog RED：`TestLoggerRecoversAfterRotationFailure` 與 `TestLoggerRecoversAfterClearFailure` 均以非空備份目錄注入檔案操作失敗，後續 Write 原先回 `file already closed`。GREEN：輪替/Clear 失敗 defer 呼叫 `reopenCurrentLocked`，成功恢復 append handle/size，復原也失敗時 `errors.Join`；eventlog 全套通過。
- TCP characterization：`TestRelayConnectionsZeroIdleTimeoutDoesNotSetTunnelDeadlines` 與 `TestRelayConnectionsHalfClosePreservesReverseTraffic` 新增後即通過，確認零逾時不設 deadline、單向 EOF 後反向資料可完成；未修改 relay 正式碼。
- 修改：`internal/proxy/udp_relay.go`、`socks5_udp_test.go`、`relay_test.go`；`internal/node/runtime.go`、`runtime_test.go`；`internal/eventlog/logger.go`、`logger_test.go`；治理契約與紀錄。`agent/項目表.md` 無需更新，因無新增檔案、模組或依賴關係。
- 驗證：改碼前基線 `go test ./...` 與 vet 全綠；最終 `go test ./... -count=1 -timeout=300s` 15 packages、`go vet ./...`、web `npm test -- --run` 13 files/73 tests、`npm run lint`、`npm run build`、Linux amd64/arm64 CGO=0 build、`git diff --check` 全通過。
- 未完整驗證：Windows 無 Linux root/network namespace，未執行真實 netlink/nftables integration；無 GCC/CGO，未執行 `go test -race`；未安裝 gopls，LSP diagnostics 無法執行。以可攜單元測試、vet 與雙架構交叉建置替代；本輪未提交或推送。
## 2026-08-28 第八輪（自主疊代：未深挖模組＋池輪換）
- 契約：§30——既有未修項＋secret/auth/stats/config/admin(HTTP/SSE/control)/cmd 深挖；B2/B3/B4 不在範圍；完成報告寫入 question.md §30.3。
- 修復 1 RED：`TestLoggerTailDoesNotBlockConcurrentWrites` 失敗訊息「Write blocked for 1.4768667s while Tail was decoding」。GREEN：`internal/eventlog/logger.go` Tail 短鎖開 fd＋size 快照、鎖外解碼、current 用 io.LimitReader(size)；eventlog 9 測試全過。
- 修復 4 RED：`TestSourcePoolReplaceWithSameAddressesKeepsRoundRobinPosition` 失敗「round robin after identical Replace = [::1 ::2 ::3 ::4], want [::4 ::1 ::2 ::3]」。GREEN：`internal/proxy/source_pool.go` Replace 開頭 identical 集合（slices.Equal）早退不重置 next；首次 GREEN 曾引入重複 p.mu.Lock 自我死鎖（TestDialerRetriesDestinationsWithOneSourceLeaseAndReleasesOnClose 240s timeout 捕獲），刪除重複 Lock 後 proxy/node 全過。
- 修復 2 RED：新增 `TestHTTPServerMutationGuardAcceptsHTTPSSameHostOrigin`（https same-host 期望 204、ftp/evil/null/空 期望 403）＋既有 guard 測試 https 斷言 403→204、called 1→2。GREEN：`internal/admin/http.go` 新增 sameHostOrigin helper（url.Parse＋scheme 白名單＋Host 相等）、RequireMutation 呼叫之；admin 全套過。
- 修復 3 RED：`TestControlServerServesSecondConnectionWhileFirstIsBusy` 失敗「second control connection was blocked by the first connection's handler」。GREEN：`internal/admin/control.go` Serve accept loop 改 `go s.handleConn(ctx, connection)`；ControlServer 全套過。
- 驗證：`go test ./... -count=1 -timeout=300s` 15 packages 全綠；`go vet ./...` 乾淨；web `npm test` 73 tests、`npm run lint`、`npm run build` 全過；Linux amd64/arm64 CGO=0 交叉 build 成功。
- 未完整驗證：同前輪（無 root/netns、無 -race、無 gopls）。

## 2026-08-28 第九輪操作記錄（自主疊代：底層缺陷深挖，無程式碼修改）

- 讀取：internal/admin/agent_document.go、operations_service.go；internal/node/manager.go（810 行）、runtime.go（RefreshBindings L315-428/drainedCallbackLocked L464-476/ForceDrainBindings/bindEndpoints）、drain_tracker.go、drain_queue.go；internal/app/service.go、connectivity.go、host_addresses.go、production_build.go（601 行）、startup_nodes.go、periodic_refresh.go、node_secrets.go；internal/node/resource_runtime.go（RuntimeResourceSynchronizer.Sync）；web/src grep EventSource/timer 模式；agent/question.md（§30/§31）、agent/deep_todos.md、agent/memory.md。
- 修改：僅治理檔——agent/question.md（§31 契約＋§31.3 完成紀錄）、agent/deep_todos.md（第九輪）、agent/memory.md（本節）；無程式碼、測試或 schema 變更。
- 死鎖線索驗證（定案無缺陷）：RefreshInboundBindings（manager.go:689-740）持 m.mu 全程呼叫 runtime.RefreshBindings；removed endpoints 於 runtime.go:398-401 登記 retiring 後鎖外觸發 callback；下游 DrainTracker.InboundDrained→mark→DrainQueue.Enqueue 為單向鎖序且不回叫 Manager；DrainQueue.Run 獨立 goroutine 於 q.mu 外呼叫 CompleteDrainedAddresses；drainedCallbackLocked 鎖內原子「檢查＋刪除」防雙重觸發。
- 驗證：`go test ./... -count=1` 15 packages 全綠、`go vet ./...` 乾淨（基線＝本輪唯一驗證，因無程式碼修改）。web 輕掃以 grep＋既有 73 測試基線為準，未重跑 lint/build（無 embed 影響）。
- 未完整驗證：同前輪（無 root/netns、無 -race、無 gopls）；B2/B3 批次化重構本輪定案不實作，等價驗證需 Linux netlink/netns 環境。