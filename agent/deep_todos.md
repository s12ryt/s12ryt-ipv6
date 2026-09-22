# 完整歷史任務

## 2026-08-03：從零建立 IPv6 代理節點管理器

- [x] 盤點空工作區與本機 Go/Node/Python 能力。
- [x] 完成協定、IPv6 資源、DNS64/NAT64、Linux 網路、防火牆、安全、UI、部署與驗收需求澄清。
- [x] 使用者確認 `agent/question.md` 所載首版契約。
- [x] 建立 Go/React 專案骨架與可執行測試基線。
- [x] 依 TDD 完成設定、IPv6 資源與目的政策。
- [x] 依 TDD 完成秘密、認證、session、日誌與統計。
- [x] 依 TDD 完成 IPv6-only DoT、DNS64 與 NAT64 健康管理。
- [x] 依 TDD 完成 Linux netlink、DAD、freebind、nftables 與交易回滾。
- [x] 依 TDD 完成 SOCKS5、HTTP、mixed、UDP relay 與節點生命週期。
- [x] 依 TDD 完成管理 API、SSE 與 Unix control socket。
- [x] 完成繁中 React SPA 與元件/瀏覽器測試。
- [x] 完成 production service 組裝、systemd、Docker、操作文件與 Linux 雙架構 build。
- [x] 完成全量回歸、品質審查與交付報告；環境限制項已明確記錄。

## 2026-08-03：VPS 全自動安裝與 Release

- [x] 以 TDD 新增安全的管理埠設定 CLI。
- [x] 以 TDD 實作 GitHub Release 一鍵安裝、升級、健康檢查與完整回滾。
- [x] 建立 GoReleaser 與 tag/manual GitHub Actions 發布流程。
- [x] 更新 systemd/offline installer 與繁中部署文件。
- [x] 執行安裝器、發布設定、Go/前端及 Linux 雙架構回歸驗證。
- [x] 更新治理紀錄並推送 GitHub。
- [x] 以 RED 測試重現手動 workflow 只建立 tag、未建立 Release 的缺陷。
- [x] 修復手動 workflow，使新 tag 或安全續跑的既有 tag 在同一次 run 直接發布。
- [x] 建立並驗證 `v0.1.2` Release、checksums、雙架構 archive 與 ELF binary。

## 2026-08-04：Web 基礎／進階模式與詞彙文件

- [x] 以 TDD 建立模式預設、保存、切換與表單狀態契約。
- [x] 實作節點、IPv6資源、網路與日誌頁的基礎模式漸進揭露。
- [x] 補齊完整README詞彙表與功能／頁面操作對照。
- [x] 執行前端完整測試、lint、build及Playwright響應式驗收。
- [x] 更新治理紀錄並完成品質審查。

## 2026-08-04：網路自動偵測與候選選單

- [x] 以 TDD 實作 Linux UP 非 loopback 介面、IPv6 地址與路由候選偵測。
- [x] 以 TDD 實作前綴衝突標記及登入保護的管理 API。
- [x] 實作資源／節點自動命名、介面與 CIDR 候選選單及自訂備援。
- [x] 實作 NAT64 自動／自訂模式與 Cloudflare／Google Resolver 預設選單。
- [x] 完成完整回歸、Linux 雙架構 build、Playwright 驗收與治理紀錄。

## 2026-08-04：Web 節點複製與 HTTP 剪貼簿備援

- [x] 以 TDD 定義標準連線 URI、入站資源解析與隨機池選擇。
- [x] 以 TDD 實作 Clipboard API、HTTP相容備援及手動複製對話框。
- [x] 在節點操作列提供獨立的連線資訊與帳密複製功能及短暫回饋。
- [x] 執行前端完整回歸、lint、build與公網HTTP情境Playwright驗收。
- [x] 完成品質審查並更新治理紀錄。

## 2026-08-04：一鍵批次建立節點與資料夾

- [x] 以 TDD 擴充節點資料夾欄位、狀態相容性與持久化交易。
- [x] 以 TDD 實作批次建立的全量預檢、獨立帳密、單次保存與失敗回滾。
- [x] 實作登入保護的批次建立、資料夾改名／移動及逐項批量操作 API。
- [x] 實作批次共用設定、逐列預覽及基礎／進階模式。
- [x] 實作節點資料夾列表、收合、整批複製、啟停與刪除 UI。
- [x] 執行完整 Go／前端回歸、Linux 雙架構 build及 Playwright 驗收。
- [x] 完成品質審查並更新治理紀錄。

## 2026-08-04：管理面板排版、自定義控制項、Modal 與動畫

- [x] 以TDD建立可及性modal、focus trap、髒表單確認與背景鎖定基礎。
- [x] 以TDD實作統一input、select、textarea與checkbox視覺元件／樣式。
- [x] 實作可收合桌面側欄、瀏覽器偏好與主頁內容排版。
- [x] 將節點與批次節點表單／寫入操作遷移至modal及三步流程。
- [x] 將IPv6資源、網路、日誌寫入與危險操作遷移至modal。
- [x] 實作原生CSS動畫與`prefers-reduced-motion`降級。
- [x] 執行完整前端回歸、lint、build與Playwright多尺寸／鍵盤驗收。
- [x] 完成品質審查並更新治理紀錄。

## 2026-08-05：AGPL 開源授權

- [x] 確認遠端公開倉庫原先沒有授權檔或授權聲明。
- [x] 完成 `AGPL-3.0-or-later`、著作權署名、同步範圍與既有 Release 邊界澄清。
- [x] 以 RED 契約測試證明根目錄缺少 GNU AGPL 授權檔。
- [x] 新增 GNU AGPL v3 全文並同步 README 與 npm SPDX 中繼資料。
- [x] 將 `LICENSE` 納入後續 GoReleaser 壓縮檔並保留第三方相依套件各自授權。
- [x] 執行 Release 契約、Go／前端完整回歸、lint、vet、build 與 GoReleaser v2 組態驗證。
- [x] 完成品質審查及治理紀錄更新。
- [x] 以五筆原子提交完成紀錄收尾並一般推送至 `origin/main`，確認本機、追蹤分支與 GitHub 遠端 SHA 一致。

## 2026-08-24：本機 Agent CLI 與一鍵安裝整合

- [x] 完成 CLI、control socket、apply/export/schema、安全、設定生效與安裝回滾需求澄清。
- [x] 將確認契約寫入 `agent/question.md` 第 24 節。
- [x] 建立相關 Go 與安裝器測試基線。
- [x] 以 TDD 擴充 4 MiB control protocol 與泛用 agent RPC。
- [x] 以 TDD 實作 agent schema、export、apply 與 settings 合併。
- [x] 以 TDD 實作完整命令式資源、節點、網路、日誌與統計操作。
- [x] 以 TDD 實作 `s12ryt-ipv6 agent ...` parser、機器輸出、錯誤與 timeout。
- [x] 以 TDD 整合一鍵安裝 agent gate、回滾與 quickstart。
- [x] 更新 README 與治理紀錄，完成完整回歸、品質審查及 Linux 雙架構建置。

## 2026-08-25：Web UI 視覺美化（自主疊代）

- [x] 取得「自主疊代升級」授權並記錄溝通偏好；契約寫入 `agent/question.md` 第 25 節。
- [x] 以 10 項視覺刷新契約測試形成 RED，GREEN 實作「Teal Console」主題。
- [x] 建立三層視覺深度、品牌漸層、狀態膠囊圓點、nav 指標條、卡片化、tabular-nums 與登入頁背景識別。
- [x] 新增內嵌 SVG favicon 消除每次載入的 404。
- [x] Playwright 40 組寬度×主題×頁面零溢出驗證、computed style 逐項證實與 console 歸零。
- [x] 完成前端、Go 全套、vet 與 Linux 雙架構回歸，更新治理紀錄並推送。

## 2026-08-25：Console／Journal 靜音代理連線 IPv6

- [x] 以 TDD 將 `proxy` 類事件改為只寫 JSONL 檔案，不再鏡射 stdout/journal。
- [x] `system`／`audit` 事件維持 stdout/journal 輸出；Web UI 日誌與 `agent logs tail` 查詢不變。
- [x] 更新 README 日誌說明與治理紀錄，完成 Go 全套回歸、vet 與雙架構交叉建置並推送。

## 2026-08-25：穩定性審查與崩潰修復

- [x] 全面掃描 goroutine 生命週期、HTTP 超時、限速器、SSE 與成長型 map；確認兩項高嚴重度缺陷。
- [x] TDD 修復 dns64 快取無上限（4096 上限＋過期優先／最早到期淘汰）。
- [x] TDD 修復代理連線 goroutine 無 panic 防護（dispatch recover，單連線錯誤化）。
- [x] 完整回歸（15 packages／vet／雙架構交叉建置）、治理紀錄更新並推送。

## 2026-08-25：穩定性第三輪深查（角落掃描，零程式碼變更）

- [x] 逐項驗證三種入站協定握手逾時（SOCKS5 L67/L95、HTTP L76/L106、mixed L49/L57）皆正確套用與清除，慢攻擊防護完備。
- [x] 驗證 HTTP 非 CONNECT 轉送（constant-time 認證、absolute-form 限制、拒 userinfo）、relayConnections idle deadline refresher、SSE validateEvent 擋換行注入與 drop-oldest 有界佇列。
- [x] 驗證 SourcePool 租借／排空／強制終止狀態機（鎖外 callback、once 防重複釋放、Attach 失敗全關 closer）。
- [x] 驗證全部正式狀態檔（config／stats／nodes／resources／ownership／admin-password）皆 temp+sync+rename 原子持久化，vault O_EXCL 一次建檔。
- [x] 驗證前端 api.ts 無重試風暴、EventSource 關閉冪等，與 main.go 訊號 context 正確貫穿。
- [x] 新增兩項中低嚴重度觀察列建議未修：eventlog Tail 持鎖全量解碼使查詢期間代理關閉事件寫入排隊（延遲 spike、無崩潰）；rotate 中途 reopen 失敗致日誌寫入持續報錯至重啟（錯誤已隔離、無 panic）。
- [x] 結論：本輪無高嚴重度缺陷，僅更新治理紀錄。

## 2026-08-25：IPv6 池新建路徑審查（瓶頸修復）

- [x] 全路徑審查新建池：ipv6resource store/template/random/state、admin ResourceCoordinator 事務（clone→mutate→Reconcile→Sync→Save→swap 三層回滾）、network manager、kernel netlink 層。結論：正確性無缺陷（引用計數交叉驗證、drain 批次單調、原子寫、回滾自癒完備）。
- [x] 發現四項規模相關瓶頸（皆隨池容量 C 平方放大）：B1 removeStale/release 每移除一地址即 fsync 重寫 ownership 檔（刪大池達數十秒、逼近 systemd 90s stop 上限）；B2 AddressExists 每次全量 AddrList dump（O(C²)）；B3 waitForDAD C 個平行輪詢各自全量 dump；B4 coordinator 單鎖涵蓋整個網路事務阻塞管理 API。
- [x] TDD 修復 B1：ownership Save 批次化（removeStale/releaseAddresses/releaseRoutes 改為迴圈後單次保存，成功移除的部分狀態仍持久化）；RED 兩測（saves=5/3）→ GREEN saves≤2/1，部分失敗語義守護測試通過。
- [x] B2/B3/B4 列建議未修（需 Kernel 介面批次化重構，收益僅在超大池）。
- [x] 完整回歷：15 packages、vet、Linux amd64/arm64 交叉建置通過；治理紀錄更新並提交。

## 2026-08-25 第五輪：IPv6 池輪換（refresh→drain）審查與修復

- 觸發：使用者要求審查「IPv6 池輪換那部份的代碼有沒有 bug 或瓶頸」。審查鏈：RefreshPool → drain_tracker/drain_terminator → DrainQueue → SourcePool Replace → OutboundRegistry.Sync → RuntimeResourceSynchronizer.Sync → 啟動序列（service.go ReconcileResources 先於 RestoreNodes 先於 RunNAT64）。
- 缺陷 R1（中高，已修）：重啟（含 crash）後 outbound 池 draining 批次永久殘留——outbound 池 consumers 恆非空且 runtime SourcePool 只含 Active，onDrained 永不觸發；批次殘留 state、地址掛網卡、UI 永遠排空中。修復：`ResourceCoordinator.CompleteAllDrains(ctx)` 單一事務完成全部殘留批次（無 draining 時 no-op），production `ReconcileResources` 閉包於 `resources.Reconcile` 前呼叫（節點 Restore 前）。
- 瓶頸 R2（中高，已修）：DrainQueue 逐地址完整 coordinator 事務（每地址 2×state 深拷貝＋全量 Reconcile＋runtime Sync＋fsync＋全程持鎖），100 地址池刷新＝100 次事務。修復：completer 介面改批次簽名 `CompleteDrainedAddresses(ctx, pool, []netip.Addr)`，DrainQueue 按池分組保序後每池一次呼叫；coordinator 批次版驗證/去重/過濾非 draining（冪等）後單一事務完成；單地址版保留並委託批次版。
- 其餘確認安全：CompleteDrainedAddress 冪等、ForceDrain 與積壓消費交錯無害、registry.draining 與 state 同步提交、Prepare/mark 鎖外回呼無競態、Enqueue wake 緩衝 1 無丟失、三層回滾正確。觀察（低）store.go automaticCount==0 覆蓋 err 的怪異寫法無實害，不修。
- TDD：RED＝drain_queue_test.go 批次介面編譯失敗＋resource_service_test.go 新方法不存在編譯失敗；GREEN＝app/admin 全綠。新測試：按池分組保序、單一事務 saves=1、混合已完成/重複地址冪等、無 draining no-op、CompleteAllDrains 雙批次單事務、無效輸入拒絕。
- 驗證：`go build ./...`、`go vet ./...`、`go test ./...`（15 packages）全綠；Linux amd64/arm64 交叉建置通過。提交：fix(resources) 批次完成＋啟動清殘＋docs。

## 2026-08-25 第六輪：同類缺陷模式全面排查（F1/F2 修復）

- 觸發：使用者要求翻找其他類似 R1/R2/B1/B2-B3 同型缺陷（逐項事務/重啟殘留/O(n^2) dump/無界累積/資料路徑全量替換）。
- 掃描安全結論：stats 持久化（ticker 間隔保存非每連線）、node persistent（每操作單次 Save＋回滾）、eventlog Write（append 無 fsync、proxy 不入 stdout）、agent apply 逐項事務（§24 契約設計）、operations 迴圈（DTO 轉換）、connectivity/host_addresses/node_secrets（on-demand）、dns64 monitor（60s ticker＋Stop）、agent_commands createNodeBatch（CreateBatch 單事務冪等）、vault（啟動一次 O_EXCL）、config 純函數。
- F1（中高，資料路徑）：每 UDP ASSOCIATE 兩次完整 nftables 全表替換。修復：Opening.PortEnd＋backend Gte/Lte＋FirewallCoordinator relayScope{family,address} 計數（首次/末次才 Replace）＋openings() 每 scope 一條 UDP 埠範圍規則＋production 以 settings.Ports.Min/Max 接線。TDD：manager 3 測試＋coordinator_test 全改寫（ReferenceCountsRelayScopesAcrossPorts/TracksRelayScopesPerAddress/ValidatesConstruction 擴充）＋backend 範圍表達式測試；RED=編譯失敗（PortEnd/新簽名），GREEN=Windows 三套件綠＋WSL linux binary PASS。
- F2（中，資料路徑）：Policy() 每出站連線 clone 兩個地址集。修復（契約變更 snapshot→唯讀視圖）：Policy() 免 clone 回傳共享引用；DestinationPolicy/Policy() 文檔明示唯讀；grep 全 codebase 零寫入消費者。TDD：ZeroCopy identity 測試 RED（clone 版 fail）→GREEN；swap 語義守護＋併發功能測試；既有 mutation 防護斷言改寫為新契約。
- 殘餘：-race 無 cgo/gcc 環境不可執行（結構性論證＋功能渉試替代）；真實 netlink 行為列 integration 風險。
- 迴歸：go vet＋15 packages 全綠＋Linux amd64/arm64 交叉建置。

## 2026-08-28 第七輪：後端核心與代理長連線穩定性

- [x] 依 `agent/question.md` §29 稽核全 Go 後端核心，優先追查 SOCKS5 TCP/UDP、HTTP CONNECT 與 mixed 長連線；改碼前基線 `go test ./...` 15 packages 與 `go vet ./...` 全通過。
- [x] 修復 UDP association 只由 client datagram 刷新 idle deadline，導致 remote-only 活動仍固定逾時；遠端成功回應現在同步刷新 association deadline，刷新失敗保留原始錯誤並讓主迴圈收斂。
- [x] 修復 UDP 回寫 client 失敗後 mapping 未移除，以及 packet deadline 設定失敗被吞沒；兩者皆有獨立 RED 回歸測試。
- [x] 修復 running node 的 no-op 與純 Name/Folder 更新無條件重建 handler、切斷 active 長連線；等價 runtime 設定走 metadata fast path，認證、限制、逾時、資源或入站設定變更仍重建並關閉舊 session。
- [x] 修復 eventlog rotation 與 Clear 在中途檔案操作失敗後留下 closed file handle；失敗仍回報，並以 append reopen 恢復 logger 可用性，復原失敗以 `errors.Join` 保留雙重原因。
- [x] 補 TCP relay 行為特徵測試：idle timeout=0 不設定 tunnel deadline；half-close 後反向流量仍可完成。兩測新增後即通過，確認正式碼既有行為符合契約。
- [x] 核心掃描確認 eventlog Tail 持鎖、runtime Stop、DAD、背景 goroutine/ticker、原子狀態 store 無可穩定證明的新正確性缺陷，未做臆測性重構。
- [x] 完整驗證：`go test ./... -count=1 -timeout=300s`、`go vet ./...`、web 13 files/73 tests、lint、Vite build、Linux amd64/arm64 CGO=0 build、`git diff --check` 全通過。
- [x] 環境限制：Windows 無 root/network namespace，未跑真實 netlink/nftables integration；無 GCC/CGO，未跑 `-race`；未安裝 gopls，故以 test/vet/build 替代 LSP 診斷。

## 2026-08-28 第八輪：未深挖模組稽核與池輪換修復

- [x] 提交第七輪未提交修復：四個原子提交 fix(proxy) f7a6e7d、fix(node) 9510cc3、fix(eventlog) 6f9e82a、docs(agent) fbe35ac。
- [x] 依 `agent/question.md` §30 掃描既有未修項與前七輪未深挖模組（secret、auth、stats、config、admin HTTP/SSE/control、cmd CLI parser）；`secret`/`auth`/`stats`/`config`/`cmd` 無缺陷，management.go http.Server 無 WriteTimeout（SSE 不受影響），store `automaticCount==0` 覆蓋定案為 pinned==capacity 池的刻意補償，不修。
- [x] 修復 eventlog `Tail` 持鎖全量解碼阻塞 Write（300k 行下 Write 被 Tail 阻塞 1.47s）：短鎖內開啟全部檔段 fd＋快照 current size，鎖外解碼，current 以 io.LimitReader 防半行；RED `TestLoggerTailDoesNotBlockConcurrentWrites`。
- [x] 修復使用者回報的池輪換缺陷「輪換ipv6池怎麼都是在第一輪和第二輪來回」：任何資源事務與每條 draining 連線結束都觸發 runtime.Sync → OutboundRegistry.Sync 對既有池呼叫 `Replace(pool.Active)` → 原實作無條件 `p.next = 0` 重置 round-robin；修復為集合相同（slices.Equal）時不重置 cursor。RED `TestSourcePoolReplaceWithSameAddressesKeepsRoundRobinPosition`；過程中重複 mu.Lock 自我死鎖由既有 dialer 測試（changed 路徑）捕獲修正。
- [x] 修復 admin `RequireMutation` Origin 檢查硬編碼 http scheme，HTTPS 反代（README 可信安全通道）下所有寫操作 403：改為 scheme ∈ {http,https} 且 origin.Host == request.Host；https same-host origin 契約由 403 改為放行。RED `TestHTTPServerMutationGuardAcceptsHTTPSSameHostOrigin`。
- [x] 修復 admin control `Serve` accept loop 同步 handleConn，長 agent apply（10 分鐘）阻塞後續 control 連線（安裝器 120s 健康檢查誤判回滾）：改為 goroutine-per-connection，ctx 取消仍中止每條連線。RED `TestControlServerServesSecondConnectionWhileFirstIsBusy`。
- [x] 完整驗證：`go test ./... -count=1 -timeout=300s` 15 packages、`go vet ./...`、web 73 tests、lint、build、Linux amd64/arm64 CGO=0 build 全通過。
- [x] 環境限制同第七輪：無 root/netns（integration 未跑）、無 cgo/gcc（-race 未跑）、無 gopls。

## 2026-08-28 第九輪：底層缺陷深挖（歷輪覆蓋最少區域）

- [x] 契約 §31 寫入 `agent/question.md`：優先正確性缺陷，集中在 admin agent_document/operations_service → node manager/runtime → app 生命週期 → web 輕掃；B2/B3 需決定性等價測試才可本輪處理。
- [x] 基線全綠：`go test ./... -count=1` 15 packages、`go vet ./...` 乾淨。
- [x] 深挖 `internal/admin`：agent_document.go（欄位級合併/Validate/export preserve 正確）、operations_service.go（回滾 errors.Join 完整、Overview cancel 無洩漏）——無缺陷。agent.go apply 事務屬前輪已深挖，不重掃。
- [x] 深挖 `internal/node`：manager.go 全檔＋runtime.go RefreshBindings/drain 回呼鏈＋drain_tracker/drain_queue 鎖序。重點線索「RefreshInboundBindings 持 m.mu 下同步觸發 onDrained 是否死鎖」定案：callback 僅入 DrainTracker（鎖序單向 m.mu → DrainTracker.mu → DrainQueue.mu，不回叫 Manager）；DrainQueue.Run 鎖外才取資源鎖；drainedCallbackLocked 鎖內原子檢查＋刪除 retiring，防雙重觸發與過早排空——無死鎖、無缺陷。
- [x] 深挖 `internal/app`：service.go（results cap=3、closeListeners 冪等、cleanup 順序、InitializeRuntime 失敗僅 ShutdownFirewall 合理）、connectivity.go、host_addresses.go、production_build.go（601 行：nftables 無殘留路徑、logger 雙關閉冪等、RestoreNodes 全節點 RegisterSecret 防洩漏、RunNAT64 裸 goroutine 隨 ctx、prepareControlSocket 拒非 socket、close once）、startup_nodes.go、periodic_refresh.go、node_secrets.go——無缺陷。
- [x] 深挖 `web` 輕掃：EventSource 僅 api.ts:221（round8 已深掃）、無 setInterval、copyTimer clearTimeout 保護完整；73 前端測試基線全綠——無新缺陷。
- [x] B2/B3 決策：本輪不實作（效能重構非正確性；需改 Kernel 介面＋linuxKernel＋waitForDAD＋fake kernel 全鏈；等價驗證需 Linux netlink/netns 環境，Windows 無法執行 integration）。留待下輪專項。
- [x] 本輪結論：未發現新的正確性缺陷；無程式碼修改，基線（go test 15 packages＋vet）即為驗證；治理檔更新後提交 docs(agent)。

## 2026-08-28 第十輪：B2/B3 批次查詢重構（fix(network)）

- [x] 契約 §32（使用者「開修吧」授權）：B2=AddressExists O(C²)→每介面一次 dump 建集合；B3=waitForDAD 共享單一輪詢器；錯誤語意/回滾順序/逾時上限逐字等價；先 characterization 後重構；B4 不在範圍。
- [x] RED：6 新測試（manager 層 Apply/Reconcile 批次計數斷言；kernel_linux 層 InterfaceAddresses/單輪詢器/DAD 聚合/dump 錯誤傳播）→ WSL `go test ./internal/network` 編譯失敗（InterfaceAddresses/WaitAddressesReady undefined 5 處）＝缺方法 RED。
- [x] GREEN：manager.go Kernel 介面＋interfaceAddressSets helper＋三處 AddressExists 批次化（錯誤格式不變）＋waitForDAD 委派 WaitAddressesReady；kernel_linux.go 兩方法（InterfaceAddresses 一次 dump；WaitAddressesReady 按介面分組、單一 ticker、DADFAILED 聚合、失敗/逾時對剩餘 refs 各附 Canceled/ctx.Err()，per-ref 包裝逐字等價）；3 次迭代後 Windows+WSL network 全綠。
- [x] 量測證據：Apply 3 地址 AddressExists 3→0、WaitAddressReady 3→0（改 InterfaceAddresses 1＋WaitAddressesReady 1）；dump O(C)→O(1)/介面、DAD 輪詢 O(C)→O(1)/tick。
- [x] 連鎖修復：app production_build_test.go productionTestKernel 補 InterfaceAddresses/WaitAddressesReady 兩 stub（Kernel 介面新增方法破壞既有 fake）。
- [x] 完整回歸：Windows go test ./... 15 packages＋vet 乾淨；WSL Linux network/app/node/firewall/eventlog 全綠（admin flaky 重跑通過）；web 73 tests＋lint＋build 全過；Linux amd64/arm64 CGO=0 交叉 build 雙架構成功。
- [x] WSL2 環境限制定案：proxy TestRelayConnectionsHalfClosePreservesReverseTraffic 系統性 flaky（connection refused，雙 conn pair；Windows 10/10 穩定；與本輪無關）不修，真機 Linux 驗證留待後續；-race/integration 環境不可用照舊。

## 2026-08-29 第十一輪：底層深挖＋三項低成本防禦修復

- [x] 契約 §33（使用者「自主疊代升級，底層還有 bug」）：深挖 dns64/policy/network discovery/firewall；歷輪殘留低成本建議項一併修（control panic 防護、stats registry 殘留、eventlog RegisterSecret 成長）；B4 不動。
- [x] 深挖結論：無新確定性缺陷。dns64（failover/TTL/evict/RFC7050/monitor 鎖序/literal NAT64 驗證）、policy（目的政策順序/special ranges/decodeNAT64 /96）、discovery、firewall 診斷均穩健；dns64 cache stampede（併發同 key 重複上游查詢）屬效能觀察列建議。
- [x] 修復 1：stats.Registry.RemoveNode（鎖內 delete、空 ID no-op、與 ResetNode 語意區分）。RED=TestRegistryRemoveNode*2（方法不存在編譯失敗）。
- [x] 修復 2：eventlog secret 引用計數——RegisterSecret 重複值計數+1、UnregisterSecret 遞減歸零才移除、未知/空 no-op；redact 順序不變（遮蔽輸出逐字等價）；多節點同密碼不誤拆去敏。RED=TestLoggerUnregister*2。
- [x] 修復 3：node_secrets.go Delete 掛鉤——可選 statsRemover（nil 容忍）＋secretUnregistrar 型別斷言；Delete 先 Get 保留刪除前帳密，成功後反註冊 username/password＋RemoveNode(id)；失敗不清理；register-only registrar 相容。production_build 接線傳 registry。殘留語意（保守）：Update 輪換舊值與 RestoreNodes 重複註冊計數殘留至重啟，不減弱遮蔽。RED=TestSecretRegisteringNodeServiceDelete*3（建構子參數不符編譯失敗）。
- [x] 修復 4：control.go handleConn named return＋頂層 recover——panic 時 best-effort 回固定錯誤 "internal control error"（不洩漏 panic 內容）、回傳錯誤給呼叫端；recover defer 註冊於 connection.Close 後（unwind 先寫回應再關連線）；Serve goroutine 與 HandleConn 同步路徑同受保護。RED=TestControlServerHandleConnRecoversFromHandlerPanic（panic 崩潰測試進程）。
- [x] 回歸：go vet 乾淨；go test ./... -count=1 -timeout=300s 15 packages 全綠；本次變更檔案 gofmt 乾淨（http_test.go/manager_test.go/firewall_coordinator.go 為基線既有偏離，不在 diff 不動）；Linux amd64 CGO=0 build 成功。
- [x] 環境限制照舊：無 root/netns（integration 未跑）、無 cgo（-race 未跑）；arm64 交叉 build 未重跑（同機制，amd64 已驗證）。

## 2026-08-29 第十二輪：底層深挖＋S1/S2 殘留項收尾

- [x] 契約 §34（使用者「自主疊代升級,底層還有 bug」）：深挖 node inbound/outbound/resolved_runtime/resource_runtime/udp_factory/handler_builder、proxy port_allocator/socket_system/http_proxy、admin nodes/resources/operations/password_store/reset_password、app traffic_observer/health/statistics/deferred_*/startup_state/config_store、前十/十一輪新碼複查；殘留 S1/S2 修復；B4 不動。
- [x] 基線全綠（15 packages＋vet）。深挖 A-E 全部完成，無新缺陷；觀察項：dual 棧＋空 active 池僅聽 IPv4（既有行為）、ReleaseEndpoints Close 失敗仍移除（無法穩定重現）、非 CONNECT 轉送無 idle timeout（契約未要求）、WaitAddressesReady 全就緒後仍輪詢 AddrList（啟動期短暫，不修）。
- [x] 修復 S1：dns64 cache stampede——Resolver 新增 inFlight map＋lookupCall；lookup cache miss 後 leader 以 queryEndpoints（原 failover/TTL 語意逐字保留）查詢、followers 共享結果/錯誤並尊重自身 ctx；不持鎖查詢、零新依賴。RED 兩測（blockingQueryer 8 併發同 key，修復前上游 8 次）。
- [x] 修復 S2：secret 註冊計數殘留——Update 前 Get 捕獲舊 node，成功後先 unregister 舊值再 register 新值（不變淨零、輪換歸零、共用密碼語意保持）。RED 兩測（countingRegistrar 生命週期歸零；修復前輪換殘留 1、不變殘留 2）。
- [x] 完整回歷：go test ./... -count=1 15 packages、vet、Linux amd64/arm64 CGO=0 交叉 build 全過。環境限制照舊（無 root/netns、無 -race）；前端未動未重跑。
## 2026-08-29 第十三輪：底層深挖＋apply prune 專用池幻影失敗修復

- [x] 契約 §35（使用者「自主疊代升級,我覺得代碼底層還有bug」）：深挖 ipv6resource/auth/app 剩餘檔案/admin frontend/agent_commands.go（歷輪覆蓋最少）；覆蓋率導向盲區掃找未測路徑；B4 不動。
- [x] 基線全綠（15 packages＋vet）。深挖結論：ipv6resource（template/random/store/state/state_store 逐行——引用計數、邊界、原子寫全穩健）、auth（session/limiter＋登入鏈）、app paths/policy_provider/management、admin frontend、agent_commands.go（704 行確認矩陣/錯誤映射/凍結快照模式）——均無新缺陷。
- [x] 覆蓋率掃描發現 `pruneResources` 0.0%（apply --prune 資源修剪自 2026-08-24 以來零測試覆蓋）→ 逐行深挖發現缺陷 D1。
- [x] 修復 D1（中）：apply `--prune` 同時刪「專用池節點＋其專用池」時，pruneNodes 連帶清理專用池後，pruneResources 拿舊快照對已消失池呼叫 DeletePool → "does not exist" → 誤報 operation_failed 並中斷後續修剪。修復：刪除前以最新 Snapshot 建存在集合，已消失視為意圖達成跳過。RED `TestAgentServiceApplyPruneToleratesPoolRemovedWithDedicatedNode`（stateful fake 模擬 coordinator 動態存在性＋manager 連帶清理）→ GREEN；場景 C 已有 preflightAgentNodeResources prune 防護確認。
- [x] 歷輪殘留觀察項複查（四項定案不修）：空 active 池為不可達防禦分支（store 三路徑保證 len(Active)==Capacity≥1）；ReleaseEndpoints Close 失敗為 best-effort＋Allocate 實測 bind 兜底；非 CONNECT 轉送實與 CONNECT 共用同一 tunnelIdleTimeout（idle=0 為第七輪鎖定契約）；WaitAddressesReady 就緒確認需至少一次 dump 屬必要成本。
- [x] 完整回歸：go test ./... 15 packages、vet、gofmt、Linux amd64/arm64 CGO=0 交叉 build 全過。環境限制照舊（無 root/netns、無 -race）；前端未動未重跑。

## 2026-08-29 第十四輪：穩定性定向巡察（競態檢測首跑）

- [x] 契約 §36：使用者澄清「無具體症狀、預防性巡察」；主軸為 WSL gcc 首跑全套 `go test -race`（第二至十三輪從未執行的最大動態驗證缺口）。
- [x] WSL `-race -count=1` 與 `-race -count=2` 兩輪全套：`WARNING: DATA RACE` = 0，歷輪鎖紀律經機械驗證健全。
- [x] 3 個測試失敗（admin×2＋proxy half-close）定案 WSL2 `virtioproxy` 環境故障：純 stdlib 重現 1979/2000（98.9%）connection refused；Windows `-count=1`/`-count=2` 全綠替代證明；第十輪 half-close flaky 機制完全解釋。
- [x] Windows `-count=2` 全套 15 packages 全綠：測試冪等性/隔離性驗證通過。
- [x] 品質修正：3 個既有 gofmt 未格式化檔案（admin/http_test.go、network/manager_test.go、node/firewall_coordinator.go）機械修正，全套 `gofmt -l` 清空（TDD 例外：純格式零行為差異，替代驗證＝gofmt 空輸出＋三包測試＋全套回歸）。
- [x] 完整回歸：Windows全套、`go vet`、gofmt、Linux amd64/arm64 CGO=0 build 全過；前端未動未重跑。
- [x] 環境限制更新：`-race` 已可於 WSL 執行且競態偵測可信；WSL 立即 loopback dial 測試不可信；無 root/netns 照舊。

## 2026-09-04 第十五輪：長流量後代理資料面停服修復

- [x] 契約 §37：依使用者授權自主調查「2C4G VPS 約 60 GB 後代理掛、Web 仍可開」；以資料面 listener、資源生命週期與計數累積為最小範圍，不建立流量配額、不改控制面。
- [x] 根因級缺陷：`listenerRuntime.accept` 對任何 Accept 錯誤直接永久退出；暫時性 Linux socket/FD 壓力可使代理不再接新連線，而管理 Web 獨立存活。
- [x] RED：`TestListenerRuntimeRetriesTemporaryAcceptError` 由 Runtime 公開行為注入首次暫時 Accept 錯誤，舊碼於 250 ms 超時，穩定重現後續連線永不 dispatch。
- [x] GREEN：暫時錯誤採 5 ms 起始、倍增至 1 s 的有界可中斷退避；成功 Accept 重設，永久/closed 錯誤退出。停止中斷退避與永久錯誤邊界測試加入後，三測試 `-count=5` 全綠。
- [x] 稽核 TCP/HTTP/SOCKS、UDP association/mapping、source lease、FD/goroutine 與 stats；釋放路徑完整，`int64`/`uint64` 在 60 GB 不溢位，無第二項可證實缺陷。
- [x] 回歸：鄰近三包 `-count=5`、Windows 全套 15 packages、vet、全套 gofmt、web 73 tests/lint、Linux amd64/arm64 build 全過；WSL race 的 node/stats 全過，proxy 僅既知 virtioproxy loopback 環境失敗。
- [x] 未完整驗證：無真實 2C4G/60 GB 長壓及 root/netns errno 壓力測試；Windows race 缺 gcc、LSP 缺 gopls。部署後需持續觀察實際流量門檻。

## 2026-09-11 第十六輪：v0.1.9 出站全失敗——O1 觀測性與 F1 LimitNOFILE 修復

- [x] 使用者回報 v0.1.9 升級後再崩：新連線/既有連線/UDP 全掛、Web running、重啟恢復；events.jsonl 大量 `connection.closed success:false error:"proxy connection failed"` 無 destination/outbound 欄位；journal 無 panic。RCA 定位 O1：traffic_observer.go 寫死失敗訊息丟棄真實錯誤鏈，無法區分 EMFILE/EADDRNOTAVAIL/DNS 候選根因；F1：systemd unit 無 LimitNOFILE（soft=1024）。
- [x] 資源生命週期審計（dialer/source_pool/socket_system/udp_relay/runtime/socks5/http relay/dot/resolver）：無明確 FD/goroutine 洩漏。
- [x] 修復 O1（TDD RED→GREEN）：write() 增加 classify 參數；dial/association 失敗記錄 `prefix + ": " + describeDialError(err)`（EMFILE/ENFILE→fd limit reached、EADDRNOTAVAIL→source address unavailable、EACCES/EPERM/ECONNREFUSED/ENETUNREACH/EHOSTUNREACH/ETIMEDOUT/ECONNRESET→穩定標籤、context/os deadline→deadline exceeded、DNSError→dns lookup not found/timeout/temporary failure/failed 且不含查詢名稱、fallback 截 200 bytes rune-safe+"..."）；rejected 分支維持原訊息不附加；不新增 Event 欄位（web 向後相容），秘密保護依賴 eventlog redact()。RED：TestTrafficObserverRecordsRealDialErrorClassification（10 cases）＋TestTrafficObserverRecordsUDPAssociationErrorClassification 修復前全失敗於寫死字串；GREEN 全過。
- [x] 修復 F1：deploy/systemd/s12ryt-ipv6.service [Service] 加 LimitNOFILE=1048576（附註解說明每連線 2+ FD）；需重裝 unit（install.sh 或 daemon-reload+restart）才生效。
- [x] 契約 §38 寫入 agent/question.md（含「不洩上游細節→記錄真實錯誤依賴 redact」決策、VPS 下次崩潰診斷指令：/proc fd 計數、limits、ip -6 addr show）。
- [x] 回歸：`go test ./... -count=1` 15 packages 全綠、`go vet ./...` 乾淨；前端未動未重跑。
- [x] 未完整驗證：無 VPS 崩潰現場 FD/limits 數據，根因（EMFILE/源地址移除/DoT）未終裁；本輪修復診斷能力與已知部署缺陷，待下次崩潰以新錯誤分類終裁。
- [x] 使用者 9/11 20:17 第二次崩潰數據：本次運行記憶體峰值僅 101.6M（排除記憶體因素）；20:18:00 重啟後 1.7s 內兩條 component.degraded（疑似 service 啟動路徑 reconcile/restore 失敗，可能指向根因 B 地址配置）；events 仍寫死錯誤（VPS 跑舊版）。據此發現並修復 O3：production_build.go report() 寫死 Error 丟棄 component/cause，改為 componentDegradedMessage()（"component degraded: " + component + ": " + truncateErrorDetail 截斷）。RED：TestComponentDegradedMessageIncludesComponentAndCause／TruncatesLongCause／TestBuildProductionRecordsComponentAndCauseInDegradedEvent（覆寫 productionTestPlatform.hostAddresses 觸發 degraded，corrupt 檔案場景不觸發 report 已改棄）；GREEN：15 packages 全綠、vet/gofmt 乾淨。
- [ ] 待辦：發新 Release（含 O1+F1+O3）升級 VPS；升級後讀 Web 總覽 Issues／`agent status` 的 degraded 細節，並於下次崩潰蒐集 /proc FD 計數、limits、`ip -6 addr show` 以終裁根因 A/B/C。

## 2026-09-11 第十七輪：push 觸發的 CI 流水線（ci.yml）

- [x] 使用者需求：「做一個CI-workflow用於在代碼推送後就可以發現有無bug的CI流水線」。盤點：倉庫僅有 tag 觸發的 release.yml，無 push CI。
- [x] 設計並寫入 .github/workflows/ci.yml（186 行）：push/PR/手動觸發、paths-ignore 文件類、concurrency 取消舊跑、permissions contents:read；五 jobs＝frontend（npm ci→lint→test→build→artifact web-dist）→backend（下載 dist→gofmt→vet→`go test -race` 全套）→build（matrix amd64/arm64 CGO_ENABLED=0 linux）＋deploy-scripts（bash -n+install_test+release_test）＋integration（needs backend；一次性 netns s12ryt-ci、trap 清理、S12RYT_INTEGRATION_NETNS=1、-tags=integration 跑 internal/network+internal/firewall）。關鍵約束：web/dist 在 .gitignore 但 //go:embed all:dist → backend/build 需 frontend artifact；network/firewall 不依賴 webui → integration 不需。
- [x] TDD 等價：actionlint RED（臨時錯誤 workflow 被抓 EXIT=1）→ GREEN（ci.yml+release.yml EXIT=0）；本地重跑 CI 命令全綠（npm lint/73 tests/build 12.49s、gofmt 空、vet 0、go test 15 packages）。
- [x] commit 3c43005 推送後首次真實 CI（run 34604069973）：全部 6 jobs success，含 -race 全套與 netns integration（GitHub ubuntu runner 證實可用）；僅 actions v4 系列 Node 20 deprecation 非阻斷警告（與 release.yml 一致，未來統一升級）。
- [x] 契約 §39 寫入 agent/question.md；項目表.md 增 ci.yml 條目。

## 2026-09-11 第十八輪：Web 面板重啟按鈕＋即時日誌

- [x] 盤點既有基建（SSE EventHub／operations API／eventlog Tail／ModalDialog／LogsView），確定 systemd 全進程重啟＋SSE log stream 方案
- [x] eventlog.Logger 訂閱廣播（buffer 64 滿則丟舊、redact 後廣播、Close 關全部；TDD 三測試）
- [x] admin：NewLogStreamHandler（GET /api/logs/stream）＋RestartService（POST /api/operations/restart，202 非同步、audit service.restart、ErrRestartUnavailable→503）
- [x] production_build 注入 restartFn=systemctl restart、SetLogStreamSource(logger.Subscribe)
- [x] 前端：api.restartService/openLogStream（isLogEvent 校驗）；LogsView 即時模式（500 上限／清空畫面）；總覽重啟按鈕（二次確認＋/healthz 輪詢）
- [x] 全套驗證：go 15 packages＋vet＋gofmt 全綠；vitest 77 tests＋lint＋build 全綠
- [ ] 待 VPS 部署新版本後實測重啟按鈕與即時日誌
### 第十八輪補記：restart 按鈕三 bug 修復（用戶質疑後推演）

- [x] Bug1：RestartService go func 移除 ctx.Done 分支（只等 timer）——request 斷線不再取消 restart；TDD cancellation 測試 RED→GREEN
- [x] Bug2：production restartFn 加 systemctl --no-block（避免被同 cgroup SIGTERM 殺）
- [x] Bug3：/healthz 加 started_at（RFC3339）＋前端 waitForServiceRestart 以 started_at 改變判定新進程（40×2s）；fetchStartedAt 記錄舊值；缺欄位降級為舊行為
- [x] Bug4：App.test /healthz mock 改 {status, started_at} 兩階段；http_test assertHealthPayload helper
- [x] 驗證：go test ./... 15 包全綠+vet+gofmt clean；vitest 77 全綠+lint+build
- [ ] 待發新 Release（v1.0.6 含 Bug1/3，重啟按鈕不可靠）；版本號待用戶指定
## 2026-09-12 第十九輪：長鏈大調查＋watchdog 自癒機制

- [x] 長鏈調查：source_pool/dialer/resolver 讀碼——內部無 FD 洩漏/池耗盡/stampede 卡死；根因排序更新 A EMFILE > D conntrack > B 地址 > C DoT
- [x] watchdog 核心 TDD（11 測試：防抖/冷卻/重啟/事件含 describeDialError 分類）
- [x] 探測器：socks5(RFC1929)+HTTP CONNECT(Basic)真實協定探測 one.one.one.one:443
- [x] production 接線：restartFn 共用+productionService watchdog 欄位+Run 掛鉤+Targets 生產函數（wildcard→loopback）
- [x] 驗證：go test ./... 15 包全綠+vet 0+gofmt 乾淨
- [ ] 待 VPS 部署後觀察 watchdog.probe 事件（若根因發作，分類錯誤即終裁數據）
## 2026-09-12 第二十輪：隨時間殘廢六項完整盤查（調查輪）

- [x] DoT queryer：每查詢新 TLS 連線無複用（架構缺陷）；resolver 快取（30s~10min+4096LRU+stampede 防護）大幅降低實際頻率
- [x] eventlog/UDP relay/TCP handler/firewall/高頻 map 五項全排除（詳 question.md §41.2）
- [x] 定論：進程內無時間退化機制；根因排序 D conntrack > A EMFILE > DoT 限流 > B 地址；待 VPS watchdog 事件終裁
- [ ] DoT 連線複用修復（決策待用戶）
## 2026-09-12 第二十一輪：DoT 連線池修復

- [x] GREEN 實作 dot.go：idle 池（takeIdle/putIdle/Close）+roundTrip（deadline 幀讀寫）+dialDoTConn（tcp6+TLS handshake 注入點）+歸還失敗自動 fresh 重試+Id 匹配驗證
- [x] 6 新測試（net.Pipe 假 DoT 伺服器：複用/重試/Id 拒絕/Close/上限/併發）
- [x] 全套 15 包全綠+vet 0+gofmt 淨
- [ ] 待 VPS 部署驗證：853 連線數穩定（ss -tnp | grep :853）、上游限流不再觸發
## 2026-09-12 v1.0.8 發佈

- tag v1.0.8（abce334）：watchdog 自癒（a07264e）＋DoT 連線池（10e7eaa）；Release workflow 34702020786 success；latest 解析 v1.0.8＋assets 齊全（checksums+amd64/arm64）
- 待用戶：VPS 一鍵升級後回報——`ss -tnp | grep :853` 連線數穩定（DoT 池生效）；watchdog.probe 事件（若根因發作自動留證）；conntrack 計數（終裁 D）

## 2026-09-12 第二十二輪：隨時間殘廢完整追查與 watchdog 修復

- [x] 重新追蹤 listener/runtime、TCP/UDP、source lease、DNS64/DoT、NAT64 monitor、資源刷新、健康聚合與 production watchdog；未找到新的進程內 FD、goroutine、cache 或 lease 無界累積證據。
- [x] 確證 v1.0.8 watchdog 實際失效：production 從宣告態 `Config.Inbound` 取 targets，但 UI/API modern node 刻意只保存 `InboundMode`/`InboundResource`，故 running nodes 得到零 targets、無 probe 事件也不會重啟。
- [x] 確證探測鏈錯誤：`one.one.one.one` 有原生 AAAA，無法強制走 DNS64/NAT64；production protocol 值為 `socks`，探測器只辨識 `socks5`，會對 SOCKS listener 錯送 HTTP CONNECT。
- [x] 確證多目標計數錯誤：單一全域 failures 會被其他健康 target 清零；改 per-target 後若每輪仍 random，目標數多時共享故障偵測延遲會按目標數放大。
- [x] TDD RED：per-target 連續失敗、IPv4 literal NAT64 目的、modern declaration resolve、production SOCKS handshake、移除 target 狀態回收、失敗 target 黏著複驗六項測試均先因原缺陷失敗。
- [x] GREEN：production 以 `InboundConfigResolver` 取得實際 bindings；目的改 `1.1.1.1:443`；接受 `socks`/legacy `socks5`；failure map 以 node/address/protocol 隔離、移除時回收，未結失敗優先連續複驗。
- [x] 驗證：`go test ./internal/app -count=1`、`go test ./... -count=1`（15 packages）、`go vet ./...`、Linux amd64 `go build ./cmd/...`、`git diff --check` 全過。
- [ ] 未完整驗證：Windows 無 gcc，無法本機執行 `go test -race`（CI 的 Linux race job可在提交後覆蓋）；無 root Linux netns/VPS 長流量現場，故原始資料面崩潰首因仍未終裁。resolver 若無法重建 running node binding 目前會略過該 target，仍缺專用事件。
- [ ] 契約更正：`nat64_prefix` 留空時程式會嘗試自動發現，並非一定沒有 NAT64；但環境確實沒有可用 NAT64 時，原生 IPv6 代理仍可運行於 degraded 狀態。固定以 `1.1.1.1:443` 失敗觸發重啟會把 NAT64 能力故障誤判為整體資料面故障，且重啟無法建立不存在的外部 NAT64。後續應將原生 IPv6 核心存活探測與 NAT64 能力診斷分離，NAT64 失敗不得單獨成為服務重啟依據。

## 2026-09-13 第二十三輪：watchdog 多路徑綜合探測

- [x] 更正單一 IPv4 literal 契約：原生資料面固定循序探測 `one.one.one.one:443`、`dns.google:443`、`example.com:443` 三個具 AAAA 的跨營運方網域；任一可達即不把節點判為整體失效。
- [x] DNS64/NAT64 條件式探測：手動 NAT64 前綴存在，或自動 monitor 為 healthy 且前綴有效時，額外探測 A-only `ipv4.google.com:443` 與 IPv4 literal `1.1.1.1:443`；未確認 NAT64 可用的環境不會因這兩項失敗進入重啟循環。
- [x] 綜合判定：每輪實際執行全部目的，只有全部失敗才回傳 watchdog probe error；各目的錯誤以固定順序聚合，NAT64 個別退化仍由既有 monitor 呈現。
- [x] 容量與時效：目的探測改為循序，避免合法 `MaxTCP=1` 節點被 watchdog 自身並行連線擠滿；每個目的按「剩餘父 deadline / 剩餘目的數」取得時間片，避免第一個慢站耗盡整輪 15 秒。
- [x] TDD 三輪：多 AAAA/條件式 DNS64+NAT64/聚合契約先因 API 不存在而 RED；並行實作被容量測試抓到同時 2 條連線；最小循序實作被 deadline 測試抓到首站取得完整預算，逐輪修至 GREEN。
- [x] 驗證：目標測試 `-count=20`、`go test ./... -count=1`（15 packages）、`go vet ./...`、`internal/app` coverage 76.2%、Linux amd64 production build、`git diff --check` 全過。
- [ ] 未完整驗證：本機缺 gcc，不能執行 Go race；缺 root Linux netns/VPS 長流量現場。若真實客戶連線已占滿 `MaxTCP`，外部協定式 probe 仍可能被容量限制拒絕；要完全消除此歧義需另設 runtime 內部健康通道或保留探測容量。resolver 建構 target 失敗目前仍只略過，缺專用事件。

## 2026-09-13 第二十四輪：擴充至 12 個 IPv6 站點

- [x] 使用者選定 12 個、採雙棧加 IPv6-only 組合；驗收決策寫入 `agent/question.md` §43。
- [x] 以 Cloudflare DNS `1.1.1.1` 分別查 A/AAAA，並直接連線首個 AAAA 的 TCP/443；27 個候選中選定的 12 個全部有 AAAA 且 IPv6/443 成功。
- [x] 雙棧 9 個：`one.one.one.one`、`dns.google`、`www.wikipedia.org`、`www.facebook.com`、`www.microsoft.com`、`www.debian.org`、`www.kernel.org`、`www.quad9.net`、`www.he.net`。
- [x] IPv6-only 3 個：`v6.ident.me`、`api6.ipify.org`、`ipv6.google.com`；實查均有 AAAA、無 A，且 IPv6/443 成功。
- [x] TDD RED：`TestWatchdogProbeDestinationsUseTwelveVerifiedIPv6Sites` 在舊清單僅回傳 3 個目的時失敗；GREEN 後目標 watchdog 規劃測試連跑 20 次通過。
- [x] 回歸：`go test ./... -count=1` 15 packages、`go vet ./...`、`internal/app` coverage 76.2%、Linux amd64 production build全過。
- [ ] 外部依賴風險：公開站點的 DNS 與 TCP/443 服務可能日後變更；現有任一成功聚合避免單站故障觸發重啟，但仍應在版本維護時重新查證清單。

## 2026-09-13 第二十五輪：GitHub CI 與 release 安全閘門強化

- [x] 建立 `internal/cicheck/workflow_test.go`，以 YAML 結構解析所有 workflow，鎖定 action 完整 commit SHA、精確版本註解、Ubuntu 24.04 runner、job timeout、唯讀 checkout、模組一致性、race shuffle、actionlint、dependency review 與 release 最小權限契約。
- [x] CI 新增 PR dependency review、workflow lint、npm high-severity audit、`go mod download/verify/tidy -diff`、nested `web` Go module 測試及固定版 `govulncheck`；所有第三方 action 固定到已查證的 40 字元 SHA，所有 job 加入 10 至 30 分鐘 timeout。
- [x] release 拆成 `verify` 與 `publish`：workflow 預設 `contents: read`，只有通過完整驗證後的 publish job 取得 `contents: write`；tag 判定改用 job outputs，前端產物以短期 artifact 從 verify 傳入 publish，GoReleaser action與binary分別固定v6.4.0 SHA及v2.18.1。
- [x] Go 安全基線升至 1.25.13：本機預設Go 1.26.3被 `govulncheck` 判定有6個可達標準庫漏洞；改以安全修補版1.25.13重掃為0個可達漏洞。README同步最低版本，`golang.org/x/mod` 改為直接測試相依。
- [x] 前端 lockfile修正 `js-yaml` 4.3.1至4.3.2及`nanoid` 3.3.16至3.3.19，`npm audit --audit-level=high` 已通過；剩餘2個moderate來自Vitest鏈，修復需升Vitest 5，未在本輪做破壞性升級。
- [x] 回歸發現並修正兩個測試基礎問題：release shell契約仍綁舊單job workflow；eventlog Tail測試使用100ms絕對門檻且失敗時未收回goroutine。兩者均先失敗，再改為新release契約與完成順序判定後通過。
- [x] 驗證：workflow契約連跑20次、Go 16 packages shuffle全綠、vet、module verify/tidy、nested web Go測試、actionlint、govulncheck、npm audit/lint/77 tests/build、deploy self-tests、Linux amd64/arm64 build全過；eventlog目標測試連跑20次、coverage 85.2%。
- [x] 遠端驗證：GitHub Actions run `34711454566` 已在Ubuntu完成Go race、govulncheck、linux amd64/arm64 build與真實network namespace integration；所有job成功，push事件的dependency review依契約略過。

## 2026-09-13 第二十六輪：推送與遠端 CI 閉環

- [x] 以 6 個原子提交推送 watchdog、eventlog測試、前端audit、Go工具鏈、CI/release與agent紀錄；`main` 已正常推送至 GitHub，未納入既有未追蹤 `thoughts/`。
- [x] 首次遠端 run `34710948221` 的前端、deploy、Go vet/race/govulncheck、linux amd64/arm64 build與真實netns integration全部成功；push事件的dependency review依契約略過。
- [x] 唯一失敗為 actionlint 在Ubuntu可使用ShellCheck時命中CI gofmt步驟的`SC2046`；本機未安裝ShellCheck，所以先前本機actionlint未覆蓋此規則。
- [x] TDD RED：新增`TestCIGofmtCheckAvoidsUnquotedCommandSubstitution`，在`gofmt -l $(git ls-files ...)`上穩定失敗；GREEN改為NUL分隔的`git ls-files -z | xargs -0 --no-run-if-empty gofmt -l`，保留只檢查追蹤檔且安全處理空白路徑。
- [x] follow-up提交`a1c5a6b`已推送；run `34711454566` 全綠，actionlint、race、漏洞掃描、雙架構build及netns integration均完成遠端驗收。
- [ ] 非阻擋警告：目前固定SHA所對應的checkout/setup-go/setup-node/artifact actions仍以Node.js 20建置，GitHub runner暫強制改用Node.js 24；後續需查證並升級到原生Node.js 24的major版本及完整SHA。

## 2026-09-13 第二十七輪：更新後首次節點恢復重試

- [x] 更正症狀：`v1.0.9` 更新後第一次服務進程內節點為 stopped／啟動失敗，手動重啟後恢復；官方一鍵與離線安裝器均會先停止舊服務、替換 binary／unit、`daemon-reload` 再啟動新進程，因此不是舊進程熱更新。
- [x] RCA：啟動期資源對帳與節點恢復錯誤原本只標記 degraded；`Manager.Restore` 單次啟動失敗會留下 stopped，`PersistentManager` 在非空狀態下仍標記 restored，同一進程不再重試；磁碟中的 desired running 狀態保留，所以再次重啟會重新嘗試並可能恢復。
- [x] TDD RED：`TestServiceRetriesTransientResourceReconciliationBeforeRestoringNodes` 證明資源對帳原本只執行一次；`TestManagerRestoreRetriesTransientRuntimeStartFailure` 證明開機恢復第一次 listener/runtime 錯誤後直接停止；`TestManagerStartRetriesTransientRuntimeFailure` 證明 Web／Agent 手動啟動同樣不重試。
- [x] GREEN：production 資源對帳、節點恢復與手動啟動均採最多 3 次、間隔 1 秒的有界重試；Restore 以輪次只重試失敗節點，總等待不隨節點數線性成長；context 取消會立即中止，永久錯誤仍回報 degraded 且管理面可用。
- [x] 邊界測試：覆蓋 Service 重試設定驗證、資源重試取消、Manager Start／Restore 取消、永久錯誤固定 3 次及健康節點不重啟；目標測試連跑 20 次通過。
- [x] 驗證：Go 16 packages `-shuffle=on` 全綠、`app` coverage 76.2%、`node` coverage 81.8%、`go vet ./...`、module verify/tidy、部署腳本語法與 self-tests、Linux amd64/arm64 build全過。
- [ ] 根因邊界：沒有故障當下 VPS journal，尚不能終裁第一次失敗是地址 DAD、listener、firewall、資源對帳或狀態儲存；安裝器目前也只辨識整體 healthy/degraded，無法精準區分合法 NAT64 degraded 與 desired-running 節點恢復失敗。

## 2026-09-13 第二十八輪：阻止 watchdog 跨程序重啟循環

- [x] RCA：單次 probe 成功原本會清除 failure；真正缺陷是 `lastRestart` 只存在記憶體。watchdog 執行 `systemctl restart --no-block` 後，新程序冷卻狀態歸零；若底層故障仍在，每三次失敗就會再次重啟。`watchdog.restart success=true` 只代表命令成功，不代表資料面恢復。
- [x] TDD RED 1：跨兩個 watchdog 實例共用 restart state，舊 API 完全沒有持久狀態契約而編譯失敗；GREEN 新增 one-shot-until-recovery guard，第二個程序持續失敗不重啟，實際恢復清除 guard，新的故障 episode 才能再次重啟。
- [x] TDD RED 2：DataPaths、file store及production接線不存在；GREEN 新增資料目錄 `watchdog-restart.json`，嚴格schema v1 JSON、原子寫入、Linux `0600`、冪等清除及production組裝。
- [x] 安全邊界：guard讀取或保存失敗時fail closed；重啟命令失敗時清除guard並保留記憶體cooldown；target移除會清理過期guard；guard只保存node/address/protocol/time，絕不保存帳密。
- [x] 測試：跨程序重啟鎖定／恢復後新episode、狀態load/save失敗、命令失敗、target移除、file round-trip／clear／invalid／corrupt／trailing data與路徑契約；watchdog目標與邊界測試連跑20次通過。
- [x] 驗證：Go 16 packages readonly+shuffle全綠、app coverage 76.5%、node coverage 81.8%、vet、module verify/tidy、部署腳本語法與self-tests、Linux amd64/arm64 build全過。
- [x] 遠端驗證：修復已納入提交 `929bfd5`，GitHub Actions run `34752131834` 的 actionlint、漏洞掃描、race、Linux amd64/arm64 build 與 root netns integration 全部通過；本機仍未安裝gopls且Windows缺gcc，但對應能力已有Linux CI證據。

## 2026-09-13 第二十九輪：watchdog 兩層綜合健康判定

- [x] 釐清兩層語意：單一 listener 內的 12 個原生 IPv6 目的及條件式 DNS64／NAT64 目的，原本已是全部實際執行且任一成功即健康；真正缺陷在 listener 層，舊狀態機會黏著單一失敗 target，三次後重啟整個服務。
- [x] 使用者選定 listener 層同樣採「任一成功即整體資料面健康」；單一局部 listener 故障不得重啟，所有目前 running listeners 都各自達到 failure threshold 才能要求全域重啟。
- [x] TDD RED：`TestWatchdogDoesNotRestartWhenAnyTargetIsHealthy` 觀察到一壞一好仍重啟一次；`TestWatchdogRestartsOnlyAfterEveryTargetReachesThreshold` 觀察到第一個 target 三次失敗便提前重啟；`TestWatchdogRestartGuardClearsWhenAnyTargetRecovers` 觀察到跨程序 guard 連續探測原 target 而未探測健康 target。
- [x] 動態集合 RED：`TestWatchdogClearsRestartGuardWhenNoTargetsRemain` 證明沒有 running target 時舊 guard 仍殘留，會污染日後新建立的資料面故障 episode。
- [x] GREEN：target 選擇改為優先最低失敗次數並在同級中使用注入亂數；任一 target 成功即清除全域 failures 與跨程序 guard；只有全部目前 targets 都達門檻才重啟。guard 僅代表全域 episode，保存的 target 只供診斷，不再綁定後續探測；零 targets 時清除 guard。
- [x] 驗證：全部 watchdog 測試連跑20次、Go 16 packages readonly+shuffle、app coverage 76.6%、vet、module verify/tidy、部署腳本語法與self-tests、Linux amd64/arm64 build全過。
- [x] 遠端驗證：綜合健康狀態機已隨提交 `929bfd5` 推送，GitHub Actions run `34752131834` 全綠；仍待真實VPS長時間故障episode觀察，但race與root netns integration已有Linux CI證據。

## 2026-09-13 第三十輪：啟動恢復與 watchdog 修復推送

- [x] 依英文semantic歷史拆成5個原子提交：`3476c57`節點runtime重試、`267158b`service啟動對帳重試、`929bfd5`watchdog跨程序與綜合健康狀態機、`97c3e9b`驗收契約、`5283b3a`專案紀錄。
- [x] 推送前以Go 1.25.13 readonly模式重跑16 packages shuffle、module verify/tidy、vet、部署self-tests及Linux amd64/arm64 build，全部通過。
- [x] `main`已推送至GitHub；run `34752131834` 全綠，包含前端audit/lint/tests/build、actionlint、Go漏洞掃描與race、雙架構build、真實Netlink/nftables network namespace integration。
- [ ] 非阻擋警告：固定SHA對應的部分官方Actions仍使用Node.js 20 runtime，由GitHub runner暫時強制改用Node.js 24；後續需升級至原生Node.js 24版本。

## 2026-09-22 第三十一輪：稽核三項確認缺陷修復（TDD）

- [x] 先完成全庫唯讀稽核（Go 16 套件 + web/src 15 檔），使用者指示「修吧」後以 TDD（先 RED 後 GREEN）修復 3 項確認缺陷；其餘 NIT 未動。
- [x] #1 MEDIUM proxy 半關閉失效：`internal/proxy/http_proxy.go:263` 對 destination 做 `CloseWrite()` 型別斷言，但 `bufferedConn`（mixed client 端）與 `leasedConn`（所有協定的 upstream 端）都只內嵌 `net.Conn` 介面、只額外實作 `Read`/`Close`，方法集不含 `CloseWrite` → 半關閉被默默丟棄，等 EOF 的 peer 會卡到 `TunnelIdleTimeout`（預設 0＝不設逾時）或 ctx 取消。
  - RED：新增 `internal/proxy/half_close_test.go`（`TestBufferedConnSupportsCloseWrite`、`TestLeasedConnSupportsCloseWrite`，以 `newTCPConnPair` 驗證 peer 收到 EOF）→ 兩者執行期 FAIL。
  - GREEN：新增 `internal/proxy/half_close.go`（`closeWriter` 介面、`forwardCloseWrite(net.Conn) error` 對不支援的 transport 回 nil、`(*bufferedConn).CloseWrite()`、`(*leasedConn).CloseWrite()`）。
- [x] #2 MINOR-MEDIUM UDP 關聯目的地無上限：`internal/proxy/udp_relay.go` 每個新目的地都會 dial 一個 socket，失敗只 continue（下一包重撥），`mappings` 僅在讀寫錯或 idle timeout（5m）回收 → 惡意/異常 client 可耗盡 FD。
  - RED：新增 `internal/proxy/udp_relay_test.go`（3 測試：失敗快取、TTL 過期後重試、目的地數上限）→ 因 `destinationDialFailureTTL`/`maxDestinationMappingsPerAssociation` 未定義而 build failed。
  - GREEN：新增 `maxDestinationMappingsPerAssociation = 256`、`destinationDialFailureTTL = 5s`、`errUDPDestinationLimit`、`errUDPDestinationUnavailable`；`udpAssociation` 加 `failures map[string]time.Time`；`mapping()` 先查既有 mapping → 負快取 → 上限檢查 → 才 dial，失敗寫入負快取、成功刪除；`closeMappings()` 清空 failures。（`git diff --numstat`：37 新增 / 0 刪除）
- [x] #3 LOW 前端日誌重掛：後端 `internal/admin/operations.go` 清日誌後發布 `Resource:"log"` 事件，`web/src/App.tsx` 的 `refresh()` 會 `setLogRevision(+1)`，而 LogsView 以 `key={logRevision}` 掛載 → 每次清除全部日誌都整檔重掛，即時日誌被關閉、篩選與對話框狀態重置。
  - RED：`web/src/LogsView.test.tsx` 新增「在 revision 變更時重新載入日誌但不重置即時模式」→ 1 failed | 5 passed。
  - GREEN：`App.tsx` 改傳 `revision={logRevision}`（移除 key）；`LogsView.tsx` 新增可選 `revision?: number` prop，以 `loadedRevision` ref 的 `useEffect` 在 revision 變更時呼叫 `loadLogs()`（不重掛、不重複載入、revision 未變時早退）。
- [x] 回歸：`go test ./... -mod=readonly -count=1`（16 套件全 ok）、`go vet ./...`（clean）、`go build ./...`（exit 0）、`gofmt -l internal\proxy`（無輸出）。
- [x] 回歸：`npm test`（13 檔 / 78 測試全綠）、`npm run lint`（clean）、`npm run build`（tsc -b && vite build 成功，並重建被 Go embed 的 `web/dist`）。
- [ ] 未執行 git add/commit/push（未經使用者授權）。
## 2026-09-22 第三十二輪：IP 池游標（避免 drain 完成後回收重用位址）

使用者需求（先以 question 工具確認方向，問答與驗收標準已寫入 agent/question.md）：
- 循序游標往前推進（每前綴持久化「下一個候選位置」）。
- 游標要持久化（跨服務重啟保留）。

問題：`GenerateAddresses` 永遠從前綴最低位址開始，只跳過「目前仍存在」的位址；drain 完成後舊位址自 store 移除，下一次 refresh 又取回剛釋放的位址（實務上 A/B 交替）。

RED（測試先寫、先失敗）：
- [x] `internal/ipv6resource/walk_test.go`（5 測試，只用既有識別字）→ `go test ./internal/ipv6resource/ -count=1` 得到 3 個預期失敗：
      TestRefreshPoolAdvancesAfterDrainCompletes / TestRefreshPoolWalkPositionSurvivesStateRoundTrip / TestFileStateStorePersistsWalkPosition，
      失敗訊息皆為 `pool "walk-pool" active = [2001:db8:1:: 2001:db8:1::1], want [2001:db8:1::4 2001:db8:1::5]`（證實現行會重用剛釋放的位址）。
- [x] `internal/ipv6resource/generate_walk_test.go`（3 測試）→ build failed `undefined: GenerateAddressesFrom`（4 個呼叫點）。

GREEN：
- [x] 新增 `internal/ipv6resource/walk.go`：`GenerateAddressesFrom(prefix, count, occupied, start)`（從 start 往前掃、跳過 occupied、到前綴尾端繞回開頭、掃完一輪不足則回 exhaustion 錯誤）、`(*Store).generateAutomatic(templateName, count)`、`(*Store).advanceNextAddress(prefix, generated)`（記錄 last+1，超出前綴則繞回 prefix.Addr()）。
- [x] `store.go`：Store 新增 `nextAddresses map[string]netip.Addr`、NewStore 初始化、CreatePool/RefreshPool 改用 `s.generateAutomatic(...)`（並移除兩個殘留的 `if ... { x, err = nil, nil }` 區塊）、DeleteTemplate 同步刪除游標。
- [x] `state.go`：State 新增 `NextAddresses map[string]netip.Addr`（yaml `next_addresses,omitempty`）、stateLocked 複製、ReplaceState 同步、buildStoreFromState 驗證游標（非 canonical prefix 或超出前綴 → 錯誤；找不到對應 template → 容忍忽略，避免舊檔無法載入）。
- [x] `state_store.go`：stateFile 新增同欄位、stateToFile/stateFromFile 轉換、補 `net/netip` import。
- [x] `gofmt -w internal/ipv6resource`（`gofmt -l` 無輸出）→ `go test ./internal/ipv6resource/ -count=1` → `ok github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource 0.942s`（8 個新測試 + 既有全部測試）。

回歸：
- [x] `go test ./... -mod=readonly -count=1` → 16 套件全部 ok（cmd 2.765s / admin 2.882s / app 11.252s / auth 0.055s / cicheck 0.096s / config 1.451s / dns64 4.777s / eventlog 2.299s / firewall 1.439s / ipv6resource 4.260s / network 10.853s / node 10.221s / policy 0.040s / proxy 11.471s / secret 0.246s / stats 0.049s）。
- [x] `go vet ./...` → clean（exit 0，無輸出）。
- [x] `git diff --numstat`：state.go +38/-0、state_store.go +10/-8、store.go +12/-12（後兩者的刪除行來自 gofmt 欄位對齊重排，已用 `git diff` 逐行確認內容符合預期，無非預期刪除）。
- [ ] 未執行 git add/commit/push（未經使用者授權）。