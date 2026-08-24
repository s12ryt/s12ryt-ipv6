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
