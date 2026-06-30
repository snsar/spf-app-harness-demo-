# Harness của project GPSR — Tài liệu thuyết trình

> Tài liệu giải thích sâu cấu trúc **harness** của project GPSR Compliance Engine.
> Mục tiêu: hiểu kỹ để thuyết trình mượt mà về project và cách setup harness.
>
> Quy ước: các thuật ngữ kỹ thuật và tên riêng (**harness, agent, skill, evidence,
> guardrail, DAG, TDD, doer/checker, false-green, deny/ask/allow, HARNESS XANH**,
> tên file/port…) được **giữ nguyên không dịch** để đúng với codebase.

---

## 1. "Harness" là gì trong project này?

**Harness** = bộ khung kỷ luật bao quanh việc Claude (AI) viết code. Nó **không phải** là app.

- **App** = `frontend/` + `backend/` + `storefront/`.
- **Harness** = mọi thứ ép AI **làm đúng quy trình, không bịa, không tự khen mình**.

Câu thần chú của cả harness (trong `CLAUDE.md`):

> **DONE = EVIDENCE.** "It works" is not evidence. Separate the doer from the checker.

Mọi thành phần đều phục vụ một trong **ba mục tiêu**:

| Mục tiêu | Ý nghĩa |
|----------|---------|
| **Định hướng** | AI biết phải làm gì, theo trình tự nào |
| **Ràng buộc** | AI không được làm gì nguy hiểm — kể cả khi nó "muốn" |
| **Bằng chứng (evidence)** | Kết quả phải kiểm chứng được, không tin lời |

---

## 2. Bốn lớp của harness

Khung tốt nhất để thuyết trình: chia harness thành **4 lớp**, từ "lời khuyên mềm" đến
"ép cứng bằng máy".

| Lớp | Bản chất | File | Ai thực thi |
|-----|----------|------|-------------|
| **L1 — Hiến pháp** | Luật bất di bất dịch (advisory nhưng tối thượng) | `CLAUDE.md` (→ `AGENTS.md`) | AI tự tuân (prompt) |
| **L2 — Quy trình** | Cách làm việc: agents + skills | `.claude/agents/`, `.claude/skills/` | AI theo hướng dẫn |
| **L3 — Bằng chứng** | Sổ sách trạng thái + feedback loop | `feature_list.json`, `progress.md`, `init.sh`, `_workspace/`, `docs/` | Người + AI đọc/ghi |
| **L4 — Rào chắn cứng** | Chặn bằng máy, không qua được | `settings.json`, `hooks/pretooluse-guard.sh` | **Máy thực thi (deterministic)** |

> **Điểm bán hàng chính:** **L1 là prompt** (AI *có thể* lờ đi), **L4 là code** (AI
> *không thể* lờ đi). `CLAUDE.md` nói rõ: *"Rules here are advisory. Deterministic
> enforcement lives in settings.json."* → triết lý **guardrail > prompt**: đừng tin AI
> nghe lời, hãy chặn nó bằng máy.

```
        mềm  ──────────────────────────────────────────────►  cứng
    ┌────────────┬──────────────┬───────────────┬──────────────────┐
    │ L1 Hiến    │ L2 Quy trình │ L3 Bằng chứng │ L4 Rào chắn cứng │
    │   pháp     │ agent+skill  │  evidence     │  settings+hook   │
    │ (prompt)   │  (prompt)    │ (người+AI)    │ (MÁY thực thi)   │
    └────────────┴──────────────┴───────────────┴──────────────────┘
     AI có thể lờ đi ◄───────────────────────────► AI KHÔNG thể lờ đi
```

---

## 3. Đi sâu từng thành phần

### L1 — `CLAUDE.md` / `AGENTS.md` (Hiến pháp)

Là một **symlink**: `CLAUDE.md → AGENTS.md`.
**Lý do (ghi trong file):** cross-model — Claude đọc `CLAUDE.md`, còn Codex/Cursor đọc
`AGENTS.md`. Một file, hai tên, mọi AI đều đọc được.

**6 Non-negotiable rules:**

1. **Design first → approve → code** — không code production trước khi duyệt thiết kế.
2. **TDD Iron Law** — không có code nào ra đời trước một test FAIL. Vi phạm → xóa làm lại.
3. **DONE = EVIDENCE** — tách **doer** khỏi **checker**.
4. **One feature in-progress at a time** — tôn trọng **DAG** phụ thuộc.
5. **Stop and ask, never guess** — gặp mơ hồ thì hỏi, không đoán.
6. **Schema only via migration** — không sửa schema sống, không đổi tên DB / port **3308**.

Ngoài ra có bảng **Harness changelog** — chính harness cũng được version như code, ghi
lại mỗi lần thay đổi cấu trúc (vd: đổi `admin/`→`frontend/`, tách `F3`→`F3`+`F3b`).

---

### L2 — Agents (5 chuyên gia)

Mỗi **agent** là một file `.md` trong `.claude/agents/` với frontmatter
(`name`, `description`, `model`). Đây là **đội ngũ ảo**.

| Agent | Vai trò | Loại |
|-------|---------|------|
| `orchestrator` | **Nhạc trưởng** — chia việc, quản DAG, giữ cổng evidence. *Không tự viết code.* | điều phối |
| `backend-engineer` | Go + Gin, MySQL, migrations, rules engine phía server | **doer** |
| `frontend-engineer` | React admin + theme extension storefront | **doer** |
| `qa-integration` | So khớp shape API↔hook, chạy verification ladder, Playwright E2E | **checker** |
| `security-reviewer` | Prompt injection, secret leak, OWASP LLM Top 10, audit settings/hook | **checker** |

**Triết lý cốt lõi: Execution mode = hybrid**

- **Build = đội** (backend + frontend tự phối hợp qua `SendMessage`).
- **Verify = sub-agent cô lập** (QA + security tách riêng — *để giữ checker trung thực*,
  không bị "ô nhiễm" bởi doer).

> **Note đáng nhắc:** cả 5 agent đổi `model: opus`→`sonnet` để **giảm token cost**, nhưng
> orchestrator được phép override lên `opus` ở các cổng rủi ro cao (`F3`/`F3b`/`F7`/`F8`)
> — nơi độ sâu adversarial thực sự bắt được lỗi (vd `F3` phát hiện lỗi HIGH: auth giả mạo
> được khi secret rỗng).

---

### L2 — Skills (quy trình đóng gói)

**Agent biết *là ai*; skill biết *làm thế nào*.** 7 skills trong `.claude/skills/`:

| Skill | Nội dung |
|-------|----------|
| **`gpsr-orchestrator`** | Bộ não — định nghĩa **luồng 6 bước** (xem dưới) |
| `gpsr-rules-engine` | Business logic cốt lõi: classify product → entity + warnings |
| `go-gin-backend` | Convention backend (Go + Gin + MySQL 3308) |
| `react-admin-shopify` | Convention frontend (React admin + theme extension) |
| `integration-qa` | Kiểm tra boundary API↔hook |
| `verification-ladder` | Chọn tầng kiểm thử theo rủi ro |
| `ai-security-review` | Review bảo mật (injection, secrets, settings/hook) |

**Luồng 6 bước của `gpsr-orchestrator`:**

- **Phase 0: Context Check** — quyết định run mode:
  - `_workspace/` chưa có → **initial run**
  - `_workspace/` có + xin fix → **partial re-run** (chỉ gọi lại agent liên quan)
  - `_workspace/` có + scope mới → **new run** (chuyển `_workspace/`→`_workspace_prev/`)
- **Phase 1: Brainstorm & Scope** — confirm WHAT + WHY, không đoán.
- **Phase 2: Plan** — viết `docs/planning/plan.md` + dựng DAG trong `feature_list.json`.
- **Phase 3: User Stories** — WHO + WHAT + WHY, acceptance criteria dạng **Given-When-Then**.
- **Phase 4: Classify Tasks** — 3 buckets: **NOW** / **COMPLEX** / **DISCUSS**.
- **Phase 5: Execute (hybrid)** — Build = đội; Verify = sub-agent cô lập.
- **Phase 6: Verify & Gate** — chỉ `done` khi có evidence thật.

> Cách phối: **agent = *who*, skill = *how-to*, orchestrator = *wiring*** (nối hai thứ lại).

---

### L3 — State files (sổ sách bằng chứng)

Bốn file giữ trạng thái giữa các phiên (vì AI mất trí nhớ mỗi session):

- **`.claude/feature_list.json`** — **trái tim của harness**. DAG các feature `F0→F9`,
  mỗi feature có `id · name · description · dependencies · status · evidence`.
  **Trường `evidence` chính là chỗ DONE=EVIDENCE được thực thi:** muốn `status: "done"`
  phải dán *output lệnh thật* vào (vd nguyên văn `go test` PASS, số test, thời gian chạy
  DB tier để chứng minh không phải skip giả).
- **`progress.md`** — nhật ký done / in-progress / blocked.
- **`session-handoff.md`** — bàn giao sạch giữa các phiên.
- **`_workspace/`** — artifact trung gian `{phase}_{agent}_{artifact}.{ext}`, giữ để audit
  (vd `F3b_security_review.md`, `F6_qa_report.md`).

Và **`docs/`** — tầng guidance:
- `docs/planning/` — `plan.md`, `user-stories.md`, `complex-cases.md`, `questions.md`
- `docs/specs/` — API/data contract per feature
- `docs/prototype/` — UI source of truth

---

### L3 — `init.sh` (feedback loop — khoản đầu tư cao ROI nhất)

`init.sh` chạy **toàn bộ chuỗi verify từ checkout sạch** và phải kết thúc xanh —
**"HARNESS XANH"**:

1. **Toolchain** (go, node, docker)
2. **MySQL** trên port **3308** (đợi healthcheck)
3. **Backend**: `go vet` → `go build` → `migrate up` → `go test` (với `GPSR_DB_TESTS=1`)
4. **Frontend**: `npm ci` → lint → test → build

**Hai chi tiết "vàng" để kể:**

- **Chống false-green:** biến `GPSR_DB_TESTS=1` ép các DB-test *phải chạy thật*; nếu DB
  không kết nối được thì **FAIL** thay vì âm thầm SKIP rồi in `ok`. Đây là bài học từ một
  lỗi thật (`F2` follow-up): test DB từng `t.Skipf` khi không có DB → vẫn `exit 0` →
  "xanh giả".
- **Không fake-green khi chưa scaffold:** nếu app chưa dựng, nó in rõ *"NOT a green build"*
  thay vì giả vờ pass.

> `CLAUDE.md` gọi đây là **merge gate** — `init.sh` xanh là điều kiện merge, và
> *"Harness debt = tech debt"* (nợ harness cũng là nợ kỹ thuật).

---

### L4 — `settings.json` (rào chắn cứng — deny/ask/allow)

Lớp **máy thực thi**, AI không vượt được. Thứ tự ưu tiên: **deny → ask → allow; deny luôn thắng.**

| Nhóm | Nội dung |
|------|----------|
| **deny** | Đọc `.env`/`*.pem`/`*.key`/`id_rsa`/`~/.ssh`/`~/.aws`; sửa chính hook hay `settings.json` (**AI không được sửa rào chắn của chính nó!**); `rm -rf`, `curl`, `wget` |
| **ask** | `git push` (phải hỏi người) |
| **allow** | `go test/build/vet`, `npm test/run/ci`, `docker compose`, `./init.sh` |

> **Minh chứng sống:** trong lúc debug, lệnh `curl` của AI từng bị **deny** đúng theo dòng
> `"Bash(curl *)"`. Đó là guardrail đang hoạt động.

---

### L4 — `hooks/pretooluse-guard.sh` (chốt chặn cuối)

Hook chạy **trước mỗi lệnh Bash** (matcher `Bash` trong `settings.json`). Nó:

- Chặn `rm -rf`/`rm -fr`, fork-bomb `:(){`, `mkfs`, `dd if=`.
- Chặn mọi lệnh shell động đến `.env`/`.pem`/`id_rsa`/`.aws/credentials` (đề phòng AI lách
  rào `Read` bằng cách `cat .env`).
- **Fail-closed:** nếu thiếu `jq` thì *chặn luôn cho an toàn* (`exit 2`), thay vì cho qua.

> `exit 2` = chặn lệnh + báo lý do về cho AI như feedback. Đây là **defense-in-depth**:
> `settings.json` chặn `Read(.env)`, hook chặn `cat .env` — hai lớp cho cùng một mối nguy.

---

## 4. Câu chuyện kết nối (slide tóm tắt)

Vòng đời một feature, từ đầu đến cuối:

```
User: "build feature X"
   │
   ▼
[orchestrator] đọc feature_list.json → chọn feature theo DAG (không skip dependency)
   │  Phase 0–4: Brainstorm → Plan(docs/planning) → User Stories(Given-When-Then) → Classify
   ▼
[BUILD = đội]  backend-engineer ⇄ frontend-engineer  (SendMessage, so khớp shape API↔hook)
   │            ↑ TDD Iron Law: test FAIL trước, code sau
   ▼
[VERIFY = sub-agent cô lập]  qa-integration (shape + ladder + Playwright)
   │                          security-reviewer (nếu high-risk: storefront/merchant input)
   ▼
[GATE]  orchestrator dán EVIDENCE thật vào feature_list.json → status=done
   │     (mismatch? → route về owner, KHÔNG mark done, re-verify)
   ▼
[init.sh]  chạy lại toàn bộ từ checkout sạch → phải "HARNESS XANH"
```

> Suốt vòng đời đó, **L4 (settings.json + hook) đứng gác nền** — bất kỳ lúc nào AI định
> đọc secret hay xóa đệ quy đều bị máy chặn ngay, bất kể prompt nói gì.

---

## 5. Ba điểm "đắt" nhất để nhấn khi thuyết trình

1. **Guardrail > Prompt.** Lời dặn (L1) AI có thể quên; rào máy (L4) thì không. Khác biệt
   cốt lõi giữa "nhắc AI cẩn thận" và "không cho AI làm bậy".

2. **Doer ≠ Checker.** Người viết code không bao giờ tự duyệt code mình. QA/security là
   sub-agent *cô lập* để trung thực. Bằng chứng thực tế: `security-reviewer` bắt được lỗi
   HIGH (auth giả mạo khi secret rỗng) mà doer không thấy.

3. **DONE = EVIDENCE + chống false-green.** `feature_list.json` bắt dán output thật;
   `init.sh` + `GPSR_DB_TESTS=1` đảm bảo "xanh" là xanh thật, không phải skip giả vờ.

---

## Phụ lục — Bản đồ file harness

```
spf-harness-app/
├── CLAUDE.md → AGENTS.md          # L1: Hiến pháp (symlink, cross-model)
├── init.sh                        # L3: feedback loop → "HARNESS XANH"
├── progress.md                    # L3: nhật ký done/in-progress/blocked
├── session-handoff.md             # L3: bàn giao giữa phiên
├── docker-compose.yml             # MySQL 8.4 → port 3308
├── shopify.app.toml               # config app ở repo root
├── _workspace/                    # L3: artifact audit ({phase}_{agent}_{artifact})
├── docs/
│   ├── planning/                  # plan, user-stories, complex-cases, questions
│   ├── specs/                     # API/data contracts per feature
│   └── prototype/                 # UI source of truth
└── .claude/
    ├── feature_list.json          # L3: DAG F0→F9 + evidence (trái tim harness)
    ├── settings.json              # L4: deny → ask → allow (MÁY thực thi)
    ├── hooks/
    │   └── pretooluse-guard.sh    # L4: chốt chặn Bash (fail-closed)
    ├── agents/                    # L2: orchestrator, backend, frontend, qa, security
    │   ├── orchestrator.md
    │   ├── backend-engineer.md
    │   ├── frontend-engineer.md
    │   ├── qa-integration.md
    │   └── security-reviewer.md
    └── skills/                    # L2: how-to đóng gói
        ├── gpsr-orchestrator/     #   bộ não — luồng 6 bước
        ├── gpsr-rules-engine/
        ├── go-gin-backend/
        ├── react-admin-shopify/
        ├── integration-qa/
        ├── verification-ladder/
        └── ai-security-review/
```
