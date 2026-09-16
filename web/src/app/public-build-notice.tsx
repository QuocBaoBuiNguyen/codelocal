"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslations } from "@/lib/i18n/provider";
import styles from "./public-build-notice.module.css";

const STORAGE_KEY = "codelocal-public-build-notice-last-shown-v1";
const DISPLAY_INTERVAL_MS = 60 * 60 * 1000;

type Copy = {
  eyebrow: string;
  title: string;
  body: string;
  body2: string;
  fork: string;
  feedback: string;
  close: string;
};

const copyByLocale: Record<string, Copy> = {
  en: {
    eyebrow: "Public build notice",
    title: "CodeLocal is still being opened up.",
    body: "codelocal.cloud is the current reference build. The public source is being manually separated and moved piece by piece from the private Enterprise codebase, which serves security-sensitive business deployments.",
    body2: "We encourage you to fork the repository, study it, adapt it, and build your own deployment. Found something wrong or have a better idea? Help us improve it in the Forums.",
    fork: "Fork on GitHub",
    feedback: "Feedback on Forums",
    close: "Continue",
  },
  vi: {
    eyebrow: "Thông báo về bản public",
    title: "CodeLocal vẫn đang trong quá trình mở mã nguồn.",
    body: "codelocal.cloud hiện là bản dựng tham chiếu của CodeLocal. Mã nguồn public đang được bóc tách thủ công và chuyển dần từng phần từ codebase Enterprise riêng, vốn phục vụ các doanh nghiệp có yêu cầu bảo mật cao.",
    body2: "Chúng tôi khuyến khích mọi người fork repository, nghiên cứu, chỉnh sửa và tự dựng phiên bản phù hợp với nhu cầu của mình. Nếu thấy điểm chưa ổn hoặc có ý tưởng tốt hơn, hãy góp ý trên Forums để cùng hoàn thiện dự án.",
    fork: "Fork trên GitHub",
    feedback: "Góp ý trên Forums",
    close: "Tiếp tục",
  },
  "zh-Hans": {
    eyebrow: "公开版本说明",
    title: "CodeLocal 仍在逐步开放源代码。",
    body: "codelocal.cloud 是 CodeLocal 团队当前的参考部署。公开源码正在从面向高安全要求企业环境的私有 Enterprise 代码库中，经过人工审查、解耦后逐步迁移出来。",
    body2: "我们鼓励大家 Fork 仓库、阅读代码、按自己的需求修改并尝试自行部署。如果你发现问题或有更好的设计，请在 Forums 中反馈，一起把项目做得更好。",
    fork: "在 GitHub 上 Fork",
    feedback: "前往 Forums 反馈",
    close: "继续",
  },
  hi: {
    eyebrow: "पब्लिक बिल्ड सूचना",
    title: "CodeLocal का ओपन-सोर्स संस्करण अभी विकसित हो रहा है।",
    body: "codelocal.cloud फिलहाल CodeLocal टीम का reference deployment है। Public source को high-security enterprise environments के लिए बने private Enterprise codebase से manually review, decouple और धीरे-धीरे migrate किया जा रहा है।",
    body2: "हम आपको repository fork करने, code पढ़ने, अपनी जरूरत के अनुसार बदलने और अपना deployment बनाने के लिए प्रोत्साहित करते हैं। कोई समस्या या बेहतर idea मिले तो Forums पर feedback दें और project को बेहतर बनाने में मदद करें।",
    fork: "GitHub पर Fork करें",
    feedback: "Forums पर Feedback दें",
    close: "जारी रखें",
  },
};

function readLastShown() {
  try {
    const value = Number(window.localStorage.getItem(STORAGE_KEY));
    return Number.isFinite(value) && value > 0 ? value : 0;
  } catch {
    return 0;
  }
}

function rememberShown(timestamp: number) {
  try {
    window.localStorage.setItem(STORAGE_KEY, String(timestamp));
  } catch {
    // Storage can be unavailable in strict privacy modes. The in-memory timer still prevents a loop.
  }
}

export function PublicBuildNotice() {
  const { locale } = useTranslations();
  const copy = useMemo(() => copyByLocale[locale] ?? copyByLocale.en, [locale]);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const timerRef = useRef<number | null>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const schedule = (delay: number) => {
      if (timerRef.current !== null) window.clearTimeout(timerRef.current);
      timerRef.current = window.setTimeout(() => {
        const now = Date.now();
        rememberShown(now);
        setOpen(true);
        schedule(DISPLAY_INTERVAL_MS);
      }, delay);
    };

    const elapsed = Date.now() - readLastShown();
    if (elapsed >= DISPLAY_INTERVAL_MS) {
      const now = Date.now();
      rememberShown(now);
      setOpen(true);
      schedule(DISPLAY_INTERVAL_MS);
    } else {
      schedule(DISPLAY_INTERVAL_MS - elapsed);
    }

    return () => {
      if (timerRef.current !== null) window.clearTimeout(timerRef.current);
    };
  }, []);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  return (
    <dialog
      ref={dialogRef}
      className={styles.dialog}
      aria-labelledby="public-build-notice-title"
      onCancel={(event) => {
        event.preventDefault();
        setOpen(false);
      }}
      onClose={() => setOpen(false)}
    >
      <div className={styles.card}>
        <div className={styles.eyebrow}>{copy.eyebrow}</div>
        <h2 id="public-build-notice-title">{copy.title}</h2>
        <p>{copy.body}</p>
        <p>{copy.body2}</p>
        <div className={styles.actions}>
          <a className={styles.primary} href="https://github.com/codelocal-cloud/codelocal" target="_blank" rel="noreferrer">{copy.fork}</a>
          <a className={styles.secondary} href="https://codelocal.cloud/forums">{copy.feedback}</a>
        </div>
        <button className={styles.close} type="button" onClick={() => setOpen(false)}>{copy.close}</button>
      </div>
    </dialog>
  );
}
