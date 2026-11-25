(() => {
  const root = document.documentElement;
  const themeToggle = document.getElementById("themeToggle");
  const themeIcon = document.getElementById("themeIcon");
  const themeLabel = document.getElementById("themeLabel");
  const heroPreviewImg = document.getElementById("heroPreviewImg");
  const heroCaption = document.getElementById("heroCaption");
  const heroTabs = Array.from(document.querySelectorAll("[data-hero-mode]"));

  function setHeroPreview(mode) {
    heroTabs.forEach((btn) => {
      btn.classList.toggle("active", btn.dataset.heroMode === mode);
    });
    if (mode === "dark") {
      heroPreviewImg.src = "images/02-dashboard-dark.png";
      heroPreviewImg.alt = "Dragon 工作台概览（深色 Mica 模式）";
      heroCaption.textContent =
        "工作台概览（深色云母模式）· 支持 Mica / Mica Alt / Acrylic 三种 Windows 11 原生透明材质（点击图片放大）";
    } else {
      heroPreviewImg.src = "images/01-dashboard.png";
      heroPreviewImg.alt = "Dragon 工作台概览（浅色 Mica 模式）";
      heroCaption.textContent =
        "工作台概览（浅色云母模式）· 一眼掌握运行中流水线、审批门禁、Issue 与发版列车（点击图片放大）";
    }
  }

  function applyTheme(theme) {
    root.setAttribute("data-theme", theme);
    localStorage.setItem("dragon-docs-theme", theme);
    if (theme === "dark") {
      themeIcon.textContent = "☀️";
      themeLabel.textContent = "浅色";
    } else {
      themeIcon.textContent = "🌙";
      themeLabel.textContent = "深色";
    }
    setHeroPreview(theme);
  }

  const savedTheme = localStorage.getItem("dragon-docs-theme");
  const prefersDark = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
  applyTheme(savedTheme || (prefersDark ? "dark" : "light"));

  themeToggle?.addEventListener("click", () => {
    const next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
    applyTheme(next);
  });

  heroTabs.forEach((btn) => {
    btn.addEventListener("click", () => {
      setHeroPreview(btn.dataset.heroMode || "light");
    });
  });

  // 7-Stage Interactive Explorer
  const STAGE_DATA = {
    requirement: {
      badge: "阶段 1 / 7 · 需求分析 (requirement)",
      title: "解析 Issue 背景、验收标准与边界条件",
      desc: "读取 GitHub Issue 正文、标签与仓库代码结构，把模糊描述拆解为可验证的验收标准清单（Acceptance Criteria），避免后续编码跑偏。",
      artifact: "spec.md",
      agent: "Claude Code (Sonnet)",
      gate: "自动通过（可自定义暂停）",
    },
    design: {
      badge: "阶段 2 / 7 · 方案设计 (design)",
      title: "梳理受影响模块、接口契约与测试策略",
      desc: "在写代码前明确修改文件清单、并发与数据迁移边界及对应单元测试设计，确保架构稳健可回溯。",
      artifact: "plan.md",
      agent: "Claude Code (Opus)",
      gate: "复杂修复模板默认在此暂停审批",
    },
    implementation: {
      badge: "阶段 3 / 7 · 编码实现 (implementation)",
      title: "在隔离 Git Worktree 中编写实现与单测",
      desc: "在同级 .dragon-worktrees 独立工作树下按 spec 与 plan 编写功能代码与单元测试，完全不触碰你主目录的代码。",
      artifact: "Worktree 分支代码提交",
      agent: "OpenAI Codex / Claude Code",
      gate: "自动流转至测试验证",
    },
    verification: {
      badge: "阶段 4 / 7 · 测试验证 (verification)",
      title: "运行构建、Lint 与测试，失败自动自愈",
      desc: "依次执行仓库配置的 buildCmd / lintCmd / testCmd；一旦失败，自动提取报错日志回传给 Agent 修复并重试（可配上限）。",
      artifact: "实时构建与测试终端日志",
      agent: "OpenAI Codex CLI",
      gate: "命令全部退出码为 0 后放行",
    },
    ai_review: {
      badge: "阶段 5 / 7 · AI 代码审查 (ai_review)",
      title: "跨 Agent 独立 Diff 审查并修正隐患",
      desc: "把完整 Git Diff 交给另一款独立 Agent（如 Gemini 2.5 Pro）审查并发、边界、错误处理与风格，并自动修复阻塞项。",
      artifact: "review.md",
      agent: "Gemini CLI (gemini-2.5-pro)",
      gate: "零阻塞项后进入 PR 提交",
    },
    pr_ci: {
      badge: "阶段 6 / 7 · 提交 PR 与 CI 跟踪 (pr_ci)",
      title: "推送分支、创建 PR 并轮询 GitHub Checks",
      desc: "自动 Push 分支、生成带关联 Issue 的 Pull Request、按开关评论原 Issue，并每 15 秒盯守远端 CI；红灯同样可自动修。",
      artifact: "PR 链接 & changes.diff",
      agent: "主路由 Agent",
      gate: "强审批模式下在提交 PR 前暂停",
    },
    merge_release: {
      badge: "阶段 7 / 7 · 合并与发布 (merge_release)",
      title: "合并 PR 并送入即时或批量发版列车",
      desc: "按仓库 Squash / Merge / Rebase 策略合并 PR；即时模式下直接发 GitHub Release，批量模式下自动进入待发版列车队列。",
      artifact: "Release Record & SemVer Tag",
      agent: "Dragon 调度器 + Release 引擎",
      gate: "默认合并前暂停，由维护者批准",
    },
  };

  const stageButtons = Array.from(document.querySelectorAll("[data-stage]"));
  const stageBadge = document.getElementById("stageBadge");
  const stageTitle = document.getElementById("stageTitle");
  const stageDesc = document.getElementById("stageDesc");
  const stageArtifact = document.getElementById("stageArtifact");
  const stageAgent = document.getElementById("stageAgent");
  const stageGate = document.getElementById("stageGate");

  stageButtons.forEach((btn) => {
    btn.addEventListener("click", () => {
      const key = btn.dataset.stage;
      const info = STAGE_DATA[key];
      if (!info) return;
      stageButtons.forEach((b) => b.classList.toggle("active", b === btn));
      stageBadge.textContent = info.badge;
      stageTitle.textContent = info.title;
      stageDesc.textContent = info.desc;
      stageArtifact.textContent = info.artifact;
      stageAgent.textContent = info.agent;
      stageGate.textContent = info.gate;
    });
  });

  // Gallery Filtering
  const galleryTabs = Array.from(document.querySelectorAll("#galleryTabs [data-cat]"));
  const galleryItems = Array.from(document.querySelectorAll("#galleryGrid .gallery-item"));

  galleryTabs.forEach((tab) => {
    tab.addEventListener("click", () => {
      const cat = tab.dataset.cat || "all";
      galleryTabs.forEach((t) => t.classList.toggle("active", t === tab));
      galleryItems.forEach((item) => {
        const match = cat === "all" || item.dataset.cat === cat;
        item.classList.toggle("hidden", !match);
      });
    });
  });

  // Lightbox Modal for all screenshots
  const lightbox = document.getElementById("lightbox");
  const lightboxImg = document.getElementById("lightboxImg");
  const lightboxTitle = document.getElementById("lightboxTitle");
  const lightboxClose = document.getElementById("lightboxClose");
  const lightboxPrev = document.getElementById("lightboxPrev");
  const lightboxNext = document.getElementById("lightboxNext");

  const zoomableTriggers = Array.from(
    document.querySelectorAll("#heroPreviewWrap, .gallery-item, .step-shot")
  );
  let activeZoomIndex = 0;

  function getTriggerMeta(el) {
    const img = el.querySelector("img");
    const titleEl = el.querySelector(".gallery-title");
    return {
      src: img?.getAttribute("src") || "",
      alt: img?.getAttribute("alt") || "",
      title: titleEl?.textContent || img?.dataset.lightboxTitle || img?.getAttribute("alt") || "预览",
    };
  }

  function openLightboxAt(idx) {
    const visibleTriggers = zoomableTriggers.filter((el) => !el.classList.contains("hidden"));
    if (visibleTriggers.length === 0) return;
    activeZoomIndex = ((idx % visibleTriggers.length) + visibleTriggers.length) % visibleTriggers.length;
    const meta = getTriggerMeta(visibleTriggers[activeZoomIndex]);
    lightboxImg.src = meta.src;
    lightboxImg.alt = meta.alt;
    lightboxTitle.textContent = `${meta.title} (${activeZoomIndex + 1}/${visibleTriggers.length})`;
    lightbox.classList.add("open");
    document.body.style.overflow = "hidden";
  }

  function closeLightbox() {
    lightbox.classList.remove("open");
    document.body.style.overflow = "";
  }

  zoomableTriggers.forEach((el) => {
    el.addEventListener("click", () => {
      const visibleTriggers = zoomableTriggers.filter((x) => !x.classList.contains("hidden"));
      const idx = visibleTriggers.indexOf(el);
      openLightboxAt(idx >= 0 ? idx : 0);
    });
  });

  lightboxClose?.addEventListener("click", closeLightbox);
  lightboxPrev?.addEventListener("click", () => openLightboxAt(activeZoomIndex - 1));
  lightboxNext?.addEventListener("click", () => openLightboxAt(activeZoomIndex + 1));
  lightbox?.addEventListener("click", (e) => {
    if (e.target === lightbox) closeLightbox();
  });

  document.addEventListener("keydown", (e) => {
    if (!lightbox.classList.contains("open")) return;
    if (e.key === "Escape") closeLightbox();
    if (e.key === "ArrowLeft") openLightboxAt(activeZoomIndex - 1);
    if (e.key === "ArrowRight") openLightboxAt(activeZoomIndex + 1);
  });

  // Copy buttons for PowerShell code blocks
  document.querySelectorAll(".code-block").forEach((block) => {
    const btn = block.querySelector(".copy-btn");
    const pre = block.querySelector("pre");
    if (!btn || !pre) return;
    btn.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(pre.textContent || "");
        const prev = btn.textContent;
        btn.textContent = "已复制 ✓";
        setTimeout(() => {
          btn.textContent = prev;
        }, 1600);
      } catch {
        btn.textContent = "请手动复制";
      }
    });
  });
})();
