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