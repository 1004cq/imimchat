/** 用户资料地区选项（分组展示 + 搜索） */
export interface RegionGroup {
  label: string;
  regions: string[];
}

export const REGION_GROUPS: RegionGroup[] = [
  {
    label: '中国 · 直辖市',
    regions: ['中国 · 北京', '中国 · 上海', '中国 · 天津', '中国 · 重庆'],
  },
  {
    label: '中国 · 华东',
    regions: [
      '中国 · 杭州', '中国 · 南京', '中国 · 苏州', '中国 · 宁波',
      '中国 · 合肥', '中国 · 福州', '中国 · 厦门', '中国 · 济南',
      '中国 · 青岛', '中国 · 南昌',
    ],
  },
  {
    label: '中国 · 华南',
    regions: [
      '中国 · 广州', '中国 · 深圳', '中国 · 东莞', '中国 · 佛山',
      '中国 · 珠海', '中国 · 南宁', '中国 · 海口', '中国 · 三亚',
    ],
  },
  {
    label: '中国 · 华中',
    regions: ['中国 · 武汉', '中国 · 长沙', '中国 · 郑州', '中国 · 洛阳'],
  },
  {
    label: '中国 · 华北',
    regions: ['中国 · 石家庄', '中国 · 太原', '中国 · 呼和浩特'],
  },
  {
    label: '中国 · 西南',
    regions: [
      '中国 · 成都', '中国 · 昆明', '中国 · 贵阳', '中国 · 拉萨',
    ],
  },
  {
    label: '中国 · 西北',
    regions: ['中国 · 西安', '中国 · 兰州', '中国 · 乌鲁木齐', '中国 · 银川'],
  },
  {
    label: '中国 · 东北',
    regions: ['中国 · 沈阳', '中国 · 大连', '中国 · 长春', '中国 · 哈尔滨'],
  },
  {
    label: '中国 · 港澳台',
    regions: ['中国 · 香港', '中国 · 澳门', '中国 · 台湾'],
  },
  {
    label: '亚洲',
    regions: ['日本', '韩国', '新加坡', '泰国', '马来西亚', '印度尼西亚', '越南', '菲律宾', '印度'],
  },
  {
    label: '欧美及其他',
    regions: [
      '美国', '加拿大', '英国', '德国', '法国', '意大利', '西班牙',
      '荷兰', '瑞士', '澳大利亚', '新西兰', '俄罗斯', '巴西', '其他',
    ],
  },
];

export const ALL_REGIONS = REGION_GROUPS.flatMap((g) => g.regions);

export function filterRegionGroups(query: string): RegionGroup[] {
  const q = query.trim().toLowerCase();
  if (!q) return REGION_GROUPS;
  return REGION_GROUPS
    .map((group) => ({
      ...group,
      regions: group.regions.filter((region) => region.toLowerCase().includes(q)),
    }))
    .filter((group) => group.regions.length > 0);
}
