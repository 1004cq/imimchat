/** 下一把 One-Time PreKey 的起始 id。必须大于已有和已分配过的 id，避免回绕后被服务端上限丢掉。 */
export function nextPreKeyStart(existingIds: number[], allocatedNext = 1): number {
  const maxExisting = existingIds.reduce((max, id) => Math.max(max, id), 0);
  const floor = Number.isFinite(allocatedNext) && allocatedNext > 0 ? allocatedNext : 1;
  return Math.max(floor, maxExisting + 1);
}
