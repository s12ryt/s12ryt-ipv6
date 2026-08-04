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
