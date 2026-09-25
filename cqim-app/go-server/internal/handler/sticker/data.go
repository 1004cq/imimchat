package sticker

// 内置贴纸数据，与 cqim-app/server/sticker.ts 的 DEFAULT_MANIFEST / DISCOVERY_CATALOG
// 逐字一致（含 emoji、名称、URL、关键词）。format 统一为 "json"。

// si 构造一个远端 json 格式贴纸条目。
func si(id, emoji, name, url string, keywords ...string) StickerItem {
	return StickerItem{
		ID:       id,
		Emoji:    emoji,
		Name:     name,
		URL:      url,
		Format:   "json",
		Keywords: keywords,
	}
}

// defaultManifestPacks 对应 sticker.ts DEFAULT_MANIFEST.packs。
func defaultManifestPacks() []StickerPack {
	return []StickerPack{
		{
			ID:          "emoji_animated",
			ShortName:   "emoji_animated",
			ShareURL:    buildShareURL("emoji_animated", ""),
			Name:        "动态表情",
			Icon:        "😀",
			Description: "默认动态表情贴纸集",
			Keywords:    []string{"默认", "动态", "情绪", "反应"},
			SourceType:  "remote",
			Stickers: []StickerItem{
				si("s1", "😀", "开心", "https://assets2.lottiefiles.com/packages/lf20_x62chJ.json", "开心", "笑脸", "高兴"),
				si("s2", "❤️", "爱心", "https://assets2.lottiefiles.com/packages/lf20_ysas4vcp.json", "爱心", "喜欢", "心动"),
				si("s3", "👍", "点赞", "https://assets3.lottiefiles.com/packages/lf20_uu0x8lqv.json", "点赞", "支持", "好"),
				si("s4", "🎉", "庆祝", "https://assets10.lottiefiles.com/packages/lf20_s2lryxtd.json", "庆祝", "派对", "礼花"),
				si("s5", "🔥", "火焰", "https://assets5.lottiefiles.com/packages/lf20_u4yrau.json", "火焰", "热烈", "燃"),
				si("s6", "⭐", "星星", "https://assets2.lottiefiles.com/packages/lf20_ysas4vcp.json", "星星", "闪耀", "高光"),
				si("s7", "🎵", "音乐", "https://assets8.lottiefiles.com/packages/lf20_kkflmtur.json", "音乐", "旋律", "音符"),
				si("s8", "💬", "聊天", "https://assets3.lottiefiles.com/packages/lf20_ydo1amjm.json", "聊天", "对话", "消息"),
			},
		},
		{
			ID:          "cute_animals",
			ShortName:   "cute_animals",
			ShareURL:    buildShareURL("cute_animals", ""),
			Name:        "可爱动物",
			Icon:        "🐱",
			Description: "默认可爱动物贴纸集",
			Keywords:    []string{"动物", "萌宠", "可爱"},
			SourceType:  "remote",
			Stickers: []StickerItem{
				si("a1", "🐱", "猫咪", "https://assets9.lottiefiles.com/packages/lf20_syqnfe7c.json", "猫咪", "可爱", "动物"),
				si("a2", "🐶", "小狗", "https://assets3.lottiefiles.com/packages/lf20_gzl797gs.json", "小狗", "狗狗", "宠物"),
				si("a3", "🐼", "熊猫", "https://assets3.lottiefiles.com/packages/lf20_gzl797gs.json", "熊猫", "国宝", "动物"),
				si("a4", "🦋", "蝴蝶", "https://assets6.lottiefiles.com/packages/lf20_OT15QW.json", "蝴蝶", "翅膀", "飞舞"),
				si("a5", "🐦", "小鸟", "https://assets6.lottiefiles.com/packages/lf20_OT15QW.json", "小鸟", "飞鸟", "动物"),
				si("a6", "🐠", "小鱼", "https://assets7.lottiefiles.com/packages/lf20_M9p23l.json", "小鱼", "海洋", "动物"),
				si("a7", "🦊", "狐狸", "https://assets4.lottiefiles.com/packages/lf20_jR229r.json", "狐狸", "萌", "动物"),
				si("a8", "🐰", "兔子", "https://assets7.lottiefiles.com/packages/lf20_M9p23l.json", "兔子", "可爱", "动物"),
			},
		},
		{
			ID:          "gestures",
			ShortName:   "gestures",
			ShareURL:    buildShareURL("gestures", ""),
			Name:        "手势动作",
			Icon:        "👋",
			Description: "默认手势动作贴纸集",
			Keywords:    []string{"打招呼", "手势", "社交"},
			SourceType:  "remote",
			Stickers: []StickerItem{
				si("g1", "👋", "挥手", "https://assets4.lottiefiles.com/packages/lf20_jR229r.json", "挥手", "打招呼", "你好"),
				si("g2", "👏", "鼓掌", "https://assets3.lottiefiles.com/packages/lf20_uu0x8lqv.json", "鼓掌", "掌声", "支持"),
				si("g3", "✌️", "胜利", "https://assets8.lottiefiles.com/packages/lf20_uu0x8lqv.json", "胜利", "比耶", "成功"),
				si("g4", "🤝", "握手", "https://assets1.lottiefiles.com/packages/lf20_obhph3sh.json", "握手", "合作", "友好"),
				si("g5", "💪", "加油", "https://assets3.lottiefiles.com/packages/lf20_touohxv0.json", "加油", "力量", "努力"),
				si("g6", "🙏", "祈祷", "https://assets7.lottiefiles.com/packages/lf20_xlmz9xwm.json", "祈祷", "感谢", "拜托"),
			},
		},
		{
			ID:          "love_romance",
			ShortName:   "love_romance",
			ShareURL:    buildShareURL("love_romance", ""),
			Name:        "爱情浪漫",
			Icon:        "💕",
			Description: "默认爱情浪漫贴纸集",
			Keywords:    []string{"爱情", "浪漫", "甜蜜"},
			SourceType:  "remote",
			Stickers: []StickerItem{
				si("l1", "💖", "心跳", "https://assets2.lottiefiles.com/packages/lf20_ysas4vcp.json", "心跳", "浪漫", "爱心"),
				si("l2", "😍", "花痴", "https://assets2.lottiefiles.com/packages/lf20_x62chJ.json", "花痴", "喜欢", "心动"),
				si("l3", "💝", "礼物心", "https://assets5.lottiefiles.com/packages/lf20_u4yrau.json", "礼物", "惊喜", "爱心"),
				si("l4", "🥰", "甜蜜", "https://assets10.lottiefiles.com/packages/lf20_s2lryxtd.json", "甜蜜", "恋爱", "开心"),
				si("l5", "💌", "情书", "https://assets3.lottiefiles.com/packages/lf20_ydo1amjm.json", "情书", "告白", "爱意"),
				si("l6", "✨", "闪耀", "https://assets2.lottiefiles.com/packages/lf20_ysas4vcp.json", "闪耀", "高光", "浪漫"),
			},
		},
	}
}

// discoveryCatalogPacks 对应 sticker.ts DISCOVERY_CATALOG。
func discoveryCatalogPacks() []StickerPack {
	return []StickerPack{
		{
			ID:          "office_boost",
			ShortName:   "OfficeBoostLab",
			ShareURL:    buildShareURL("OfficeBoostLab", ""),
			Name:        "办公室能量",
			Icon:        "💼",
			Description: "适合工作协作、日报反馈与团队庆祝的动态贴纸包。",
			Keywords:    []string{"办公", "工作", "会议", "效率", "团队"},
			SourceType:  "remote",
			Stickers: []StickerItem{
				si("ob-1", "👏", "收到", "https://assets3.lottiefiles.com/packages/lf20_uu0x8lqv.json", "收到", "确认", "办公"),
				si("ob-2", "👍", "通过", "https://assets3.lottiefiles.com/packages/lf20_uu0x8lqv.json", "通过", "同意", "赞成"),
				si("ob-3", "🔥", "冲刺", "https://assets5.lottiefiles.com/packages/lf20_u4yrau.json", "冲刺", "加速", "项目"),
				si("ob-4", "🎉", "上线", "https://assets10.lottiefiles.com/packages/lf20_s2lryxtd.json", "上线", "庆祝", "发布"),
				si("ob-5", "💬", "同步", "https://assets3.lottiefiles.com/packages/lf20_ydo1amjm.json", "同步", "沟通", "反馈"),
				si("ob-6", "⭐", "里程碑", "https://assets2.lottiefiles.com/packages/lf20_ysas4vcp.json", "里程碑", "成果", "表扬"),
			},
		},
		{
			ID:          "night_chill",
			ShortName:   "NightChillVibes",
			ShareURL:    buildShareURL("NightChillVibes", ""),
			Name:        "深夜氛围",
			Icon:        "🌙",
			Description: "适合深夜闲聊、晚安、音乐和放松场景的轻盈贴纸包。",
			Keywords:    []string{"深夜", "晚安", "放松", "音乐", "聊天"},
			SourceType:  "remote",
			Stickers: []StickerItem{
				si("nc-1", "🎵", "播放中", "https://assets8.lottiefiles.com/packages/lf20_kkflmtur.json", "音乐", "播放", "耳机"),
				si("nc-2", "🙂", "微笑晚安", "https://assets2.lottiefiles.com/packages/lf20_x62chJ.json", "晚安", "微笑", "夜晚"),
				si("nc-3", "🔥", "熬夜中", "https://assets5.lottiefiles.com/packages/lf20_u4yrau.json", "熬夜", "夜猫子", "加班"),
				si("nc-4", "❤️", "晚安心动", "https://assets2.lottiefiles.com/packages/lf20_ysas4vcp.json", "晚安", "心动", "想你"),
				si("nc-5", "💬", "再聊一会", "https://assets3.lottiefiles.com/packages/lf20_ydo1amjm.json", "继续聊", "深夜话题", "沟通"),
				si("nc-6", "✨", "月光", "https://assets2.lottiefiles.com/packages/lf20_ysas4vcp.json", "月光", "闪烁", "氛围"),
			},
		},
		{
			ID:          "meme_reactions",
			ShortName:   "MemeReactionLab",
			ShareURL:    buildShareURL("MemeReactionLab", ""),
			Name:        "梗图反应",
			Icon:        "😂",
			Description: "适合群聊高频情绪表达和轻松吐槽的动效贴纸包。",
			Keywords:    []string{"梗图", "吐槽", "搞笑", "群聊", "反应"},
			SourceType:  "remote",
			Stickers: []StickerItem{
				si("mr-1", "😂", "笑疯了", "https://assets2.lottiefiles.com/packages/lf20_x62chJ.json", "大笑", "搞笑", "哈哈"),
				si("mr-2", "🥳", "整活成功", "https://assets10.lottiefiles.com/packages/lf20_s2lryxtd.json", "整活", "成功", "庆祝"),
				si("mr-3", "🔥", "离谱但燃", "https://assets5.lottiefiles.com/packages/lf20_u4yrau.json", "离谱", "热梗", "火爆"),
				si("mr-4", "🤝", "懂你", "https://assets1.lottiefiles.com/packages/lf20_obhph3sh.json", "默契", "懂了", "好兄弟"),
				si("mr-5", "✌️", "稳住", "https://assets3.lottiefiles.com/packages/lf20_uu0x8lqv.json", "稳住", "淡定", "没事"),
				si("mr-6", "✨", "节目效果", "https://assets2.lottiefiles.com/packages/lf20_ysas4vcp.json", "节目效果", "高能", "精彩"),
			},
		},
		{
			ID:          "pet_party_club",
			ShortName:   "PetPartyClub",
			ShareURL:    buildShareURL("PetPartyClub", ""),
			Name:        "萌宠派对",
			Icon:        "🐾",
			Description: "偏治愈和卖萌的贴纸包，适合轻聊天与日常表达。",
			Keywords:    []string{"萌宠", "治愈", "日常", "卖萌", "可爱"},
			SourceType:  "remote",
			Stickers: []StickerItem{
				si("pp-1", "🐱", "猫咪围观", "https://assets9.lottiefiles.com/packages/lf20_syqnfe7c.json", "猫咪", "围观", "可爱"),
				si("pp-2", "🐶", "狗狗问好", "https://assets3.lottiefiles.com/packages/lf20_gzl797gs.json", "狗狗", "问好", "打招呼"),
				si("pp-3", "🦊", "狐狸偷看", "https://assets4.lottiefiles.com/packages/lf20_jR229r.json", "狐狸", "偷看", "萌"),
				si("pp-4", "🐰", "兔兔蹦跶", "https://assets7.lottiefiles.com/packages/lf20_M9p23l.json", "兔子", "蹦跶", "活泼"),
				si("pp-5", "🦋", "蝴蝶飞舞", "https://assets6.lottiefiles.com/packages/lf20_OT15QW.json", "蝴蝶", "自然", "梦幻"),
				si("pp-6", "🐠", "小鱼游呀游", "https://assets7.lottiefiles.com/packages/lf20_M9p23l.json", "小鱼", "海洋", "治愈"),
			},
		},
		{
			ID:          "love_confession",
			ShortName:   "LoveConfessionKit",
			ShareURL:    buildShareURL("LoveConfessionKit", ""),
			Name:        "告白合集",
			Icon:        "💘",
			Description: "适合恋爱聊天、告白和节日祝福的浪漫贴纸包。",
			Keywords:    []string{"恋爱", "告白", "心动", "浪漫", "节日"},
			SourceType:  "remote",
			Stickers: []StickerItem{
				si("lc-1", "💘", "心动一下", "https://assets2.lottiefiles.com/packages/lf20_ysas4vcp.json", "心动", "告白", "爱意"),
				si("lc-2", "😍", "好喜欢", "https://assets2.lottiefiles.com/packages/lf20_x62chJ.json", "喜欢", "爱慕", "甜蜜"),
				si("lc-3", "🔥", "热恋中", "https://assets5.lottiefiles.com/packages/lf20_u4yrau.json", "热恋", "热情", "爱情"),
				si("lc-4", "💌", "发你一封情书", "https://assets3.lottiefiles.com/packages/lf20_ydo1amjm.json", "情书", "表白", "想你"),
				si("lc-5", "🎉", "官宣啦", "https://assets10.lottiefiles.com/packages/lf20_s2lryxtd.json", "官宣", "庆祝", "恋爱"),
				si("lc-6", "✨", "甜甜发光", "https://assets2.lottiefiles.com/packages/lf20_ysas4vcp.json", "甜蜜", "闪耀", "浪漫"),
			},
		},
	}
}

// defaultManifest 构造默认 manifest（首次启动写入磁盘的内容）。
func defaultManifest() StickerManifest {
	return StickerManifest{
		Version:   1,
		UpdatedAt: nowISO(),
		Source:    "builtin-fallback",
		Packs:     defaultManifestPacks(),
	}
}
