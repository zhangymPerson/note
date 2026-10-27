// 引导页两步判据：名字与初始主密码都齐了才算引导完成。
// 抽成纯函数是因为 App.svelte 的门槛与 document.title 必须用同一个判定，
// 否则会出现「还在引导却显示『XX的笔记』」。

export type OnboardingStep = 0 | 1 | 2

export function decideStep(nameSet: boolean, masterConfigured: boolean): OnboardingStep {
  if (!nameSet) return 1
  if (!masterConfigured) return 2
  return 0
}
