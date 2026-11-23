// 分类装饰色：6 色固定色板（设计方向 C「纸墨分类彩色」）。
// 色值随主题换值（app.css 的 --c-* 变量），这里只负责按分类 id 映射变量名。
// 未分类(1) 固定灰色、速记(2) 固定墨绿；新建分类按 id 顺序循环其余 4 支，
// 保证同一分类永远同色（id 不会变，改名不换色）。
const USER_PALETTE = ["--c-work", "--c-life", "--c-read", "--c-more"] as const;

export function catColorVar(categoryId: number | null | undefined): string {
  if (!categoryId || categoryId <= 1) return "var(--c-uncat)";
  if (categoryId === 2) return "var(--c-short)";
  return `var(${USER_PALETTE[(categoryId - 3) % USER_PALETTE.length]})`;
}
