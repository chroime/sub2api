export default {
  batchImageGuide: {
    title: '图片批量生成',
    description: '一次提交多条提示词，任务完成后可统一下载图片结果'
  },
  // Home Page
  home: {
    viewOnGithub: '在 GitHub 上查看',
    viewDocs: '查看文档',
    docs: '文档',
    switchToLight: '切换到浅色模式',
    switchToDark: '切换到深色模式',
    dashboard: '控制台',
    login: '登录',
    getStarted: '立即开始',
    goToDashboard: '进入控制台',
    // 新增：面向用户的价值主张
    heroSubtitle: '一个密钥，畅用多个 AI 模型',
    heroDescription: '无需管理多个订阅账号，一站式接入 Claude、GPT、Gemini 等主流 AI 服务',
    tags: {
      subscriptionToApi: '订阅转 API',
      stickySession: '会话保持',
      realtimeBilling: '按量计费'
    },
    // 用户痛点区块
    painPoints: {
      title: '你是否也遇到这些问题？',
      items: {
        expensive: {
          title: '订阅费用高',
          desc: '每个 AI 服务都要单独订阅，每月支出越来越多'
        },
        complex: {
          title: '多账号难管理',
          desc: '不同平台的账号、密钥分散各处，管理起来很麻烦'
        },
        unstable: {
          title: '服务不稳定',
          desc: '单一账号容易触发限制，影响正常使用'
        },
        noControl: {
          title: '用量无法控制',
          desc: '不知道钱花在哪了，也无法限制团队成员的使用'
        }
      }
    },
    // 解决方案区块
    solutions: {
      title: '我们帮你解决',
      subtitle: '简单三步，开始省心使用 AI'
    },
    features: {
      unifiedGateway: '一键接入',
      unifiedGatewayDesc: '获取一个 API 密钥，即可调用所有已接入的 AI 模型，无需分别申请。',
      multiAccount: '稳定可靠',
      multiAccountDesc: '智能调度多个上游账号，自动切换和负载均衡，告别频繁报错。',
      balanceQuota: '用多少付多少',
      balanceQuotaDesc: '按实际使用量计费，支持设置配额上限，团队用量一目了然。'
    },
    // 优势对比
    comparison: {
      title: '为什么选择我们？',
      headers: {
        feature: '对比项',
        official: '官方订阅',
        us: '本平台'
      },
      items: {
        pricing: {
          feature: '付费方式',
          official: '固定月费，用不完也付',
          us: '按量付费，用多少付多少'
        },
        models: {
          feature: '模型选择',
          official: '单一服务商',
          us: '多模型随意切换'
        },
        management: {
          feature: '账号管理',
          official: '每个服务单独管理',
          us: '统一密钥，一站管理'
        },
        stability: {
          feature: '服务稳定性',
          official: '单账号易触发限制',
          us: '多账号池，自动切换'
        },
        control: {
          feature: '用量控制',
          official: '无法限制',
          us: '可设配额、查明细'
        }
      }
    },
    providers: {
      title: '已支持的 AI 模型',
      description: '一个 API，多种选择',
      supported: '已支持',
      soon: '即将推出',
      claude: 'Claude',
      gemini: 'Gemini',
      antigravity: 'Antigravity',
      more: '更多'
    },
    // CTA 区块
    cta: {
      title: '准备好开始了吗？',
      description: '注册即可获得免费试用额度，体验一站式 AI 服务',
      button: '免费注册'
    },
    footer: {
      allRightsReserved: '保留所有权利。'
    },
    public: {
    b2: {
      integrationTitle: '1 分钟接入', integrationDescription: '兼容 OpenAI SDK，只需更换 Base URL。',
      fullDocs: '查看完整文档', codeLanguage: '代码语言', copyCode: '复制代码', copied: '已复制', copyFailed: '复制失败', contacts: '联系方式'
    },
    nav: {
      apiGateway: 'API 网关', channels: '渠道', pricing: '价格', modelPlaza: '模型广场', docs: '接入文档', startBuilding: '开始构建'
    },
    contact: { joinNow: '立即加入交流群' },
    hero: {
      eyebrow: '统一模型网关', title: '一个 API，调度所有前沿模型，智能路由。',
      titleLead: '一个 API，', titleAccent: '调度所有前沿模型，', titleEnd: '智能路由。',
      subtitle: '通过兼容 OpenAI 的统一接口接入你的模型供应商，透明查看价格与运行状态。',
      createApiKey: '创建 API 密钥', exploreApi: '查看接入文档', gatewayPreview: '网关预览', keysStayYours: '密钥由你掌控', joinGroup: '加入 QQ 群',
      channels: '渠道', platforms: '平台', protocol: '协议', available: '可用 {count} 个', signInRequired: '需要登录', loading: '加载中', error: '暂时不可用', empty: '暂无渠道', notLoaded: '尚未加载', openAi: 'OpenAI',
      routingLayer: '路由层', smartFallback: '智能故障切换', visibleChannels: '可见渠道', supportedModels: '支持模型', providers: '供应商', compatibility: '兼容性', routing: '路由', openAiApi: 'OpenAI API', smartSticky: '智能 + 粘性', p50Latency: 'p50 延迟', successRate: '成功率', activeRoutes: '活跃路由', readDocs: '阅读文档'
    },
    models: { title: '支持 8 大模型厂商', pricing: '查看模型详情', app: '你的应用', appType: 'Web · App · Agent', gateway: '统一模型接入', entry: 'API 入口', platforms: '模型厂商', coverage: '已接入 8 家主流模型厂商，持续扩展中' },
    overview: {
      channelStatus: '渠道状态', available: '可用 {count} 个', signInRequired: '需要登录', loading: '加载中', error: '暂时不可用', empty: '暂无渠道', notLoaded: '尚未加载', modelsIndexed: '已收录模型', platforms: '平台数', gateway: '网关', openAiCompatible: '兼容 OpenAI'
    },
    channelStatus: {
      eyebrow: '平台覆盖', title: '渠道状态', description: '查看当前网关可用的模型供应商与分组。', platformsAvailable: '可用平台 {count} 个',
      loadingAria: '正在加载渠道状态', errorTitle: '暂时无法获取渠道状态', errorFallback: '请稍后重试。', retry: '重试',
      unavailableTitle: '渠道数据暂不可用', unavailableDescription: '公开渠道数据暂时无法读取，请稍后重试。',
      idleTitle: '渠道状态尚未加载', idleDescription: '请求公开摘要后，渠道状态会显示在这里。', emptyTitle: '暂无渠道数据', emptyDescription: '当前工作区还没有可用渠道。',
      active: '已启用', table: { platform: '平台', status: '状态', channels: '渠道', groups: '分组', models: '模型' }, noNamedChannels: '暂无命名渠道', moreChannels: '另有 {count} 个', namesNote: '展示已启用渠道的公开分组覆盖，不代表实时健康探测。', viewFullStatus: '查看模型详情'
    },
    pricing: {
      eyebrow: '模型接入', title: '模型价格与覆盖范围', description: '按平台浏览已发布的模型价格；配置价格与官方参考价会明确区分。', modelsShown: '显示 {shown} / {total} 个模型',
      loadingAria: '正在加载价格摘要', errorTitle: '暂时无法获取价格数据', errorFallback: '请稍后重试。', retry: '重试', unavailableTitle: '价格数据暂不可用', unavailableDescription: '公开价格数据暂时无法读取，请稍后重试。',
      idleTitle: '价格尚未加载', idleDescription: '请求公开模型摘要后，价格会显示在这里。', emptyTitle: '暂无已发布模型', emptyDescription: '模型广场添加模型后，价格会显示在这里。', noMatchesTitle: '没有匹配的模型', noMatchesDescription: '请更换搜索关键词或平台筛选条件。',
      searchPlaceholder: '搜索模型', platform: '平台', allPlatforms: '全部平台', clearFilters: '清除筛选', caption: '按平台展示已发布模型价格', table: { platform: '平台', model: '模型', billing: '计费方式', input: '输入', output: '输出', cacheRead: '缓存读取', groups: '分组', perToken: '美元 / token' },
      officialReference: '官方参考价', noPricePublished: '未发布价格', perRequest: '按请求', perRequestWithPrice: '按请求 {price}', tokenBilling: '按 token', reference: '参考价', notSpecified: '未指定', footerNote: '价格来自平台配置，可能因计费模式而变化。', browseModelPlaza: '浏览模型广场'
    },
    integration: {
      eyebrow: '即插即用', title: '1 分钟快速接入', description: '继续使用熟悉的 SDK，指向统一的 OpenAI 兼容 Base URL，由 {siteName} 处理供应商路由、重试与用量统计。',
      benefits: { sdk: '兼容 OpenAI 的 REST API 与 SDK', failover: '在已配置渠道之间自动故障切换', limits: '按密钥限额、日志与用量可见' }, apiReference: '查看接入文档', codeExamples: '代码示例', copy: '复制', baseUrl: 'Base URL', curl: 'cURL', javascript: 'JavaScript', python: 'Python', codePanelAria: '代码示例', codePanelFor: '{label} 代码示例'
    },
    footer: {
      builtForTeams: '为正在交付 AI 产品的团队而生。',
      brandDescription: '统一模型接入与智能路由',
      contactTitle: '联系我们',
      serviceLabel: '客服联系方式',
      copyContact: '复制联系方式',
      linksLabel: '相关链接',
      serviceStatus: '统一 API 网关 · 随时可接入'
    }
    }
  },

  // Key Usage Query Page
  keyUsage: {
    title: 'API Key 用量查询',
    subtitle: '输入您的 API Key 以查看实时消费金额与使用状态',
    placeholder: 'sk-ant-mirror-xxxxxxxxxxxx',
    query: '查询',
    querying: '查询中...',
    privacyNote: '您的 Key 仅在浏览器本地处理，不会被存储',
    dateRange: '统计范围:',
    dateRangeToday: '今日',
    dateRange7d: '7 天',
    dateRange30d: '30 天',
    dateRange90d: '90 天',
    dateRangeCustom: '自定义',
    apply: '应用',
    used: '已使用',
    detailInfo: '详细信息',
    tokenStats: 'Token 统计',
    dailyDetail: '按日明细',
    modelStats: '模型用量统计',
    // Table headers
    date: '日期',
    model: '模型',
    requests: '请求数',
    inputTokens: '输入 Tokens',
    outputTokens: '输出 Tokens',
    cacheCreationTokens: '缓存创建',
    cacheReadTokens: '缓存读取',
    cacheWriteTokens: '缓存写入',
    totalTokens: '总 Tokens',
    cost: '费用',
    // Status
    quotaMode: 'Key 限额模式',
    walletBalance: '钱包余额',
    // Ring card titles
    totalQuota: '总额度',
    limit5h: '5 小时限额',
    limitDaily: '日限额',
    limit7d: '7 天限额',
    limitWeekly: '周限额',
    limitMonthly: '月限额',
    // Detail rows
    remainingQuota: '剩余额度',
    expiresAt: '过期时间',
    todayExpires: '(今日到期)',
    daysLeft: '({days} 天)',
    usedQuota: '已用额度',
    resetNow: '即将重置',
    subscriptionType: '订阅类型',
    billingType: '计费方式',
    subscriptionExpires: '订阅到期',
    // Usage stat cells
    todayRequests: '今日请求',
    todayInputTokens: '今日输入',
    todayOutputTokens: '今日输出',
    todayTokens: '今日 Tokens',
    todayCacheCreation: '今日缓存创建',
    todayCacheRead: '今日缓存读取',
    todayCost: '今日费用',
    rpmTpm: 'RPM / TPM',
    totalRequests: '累计请求',
    totalInputTokens: '累计输入',
    totalOutputTokens: '累计输出',
    totalTokensLabel: '累计 Tokens',
    totalCacheCreation: '累计缓存创建',
    totalCacheRead: '累计缓存读取',
    totalCost: '累计费用',
    avgDuration: '平均耗时',
    // Messages
    enterApiKey: '请输入 API Key',
    querySuccess: '查询成功',
    queryFailed: '查询失败',
    queryFailedRetry: '查询失败，请稍后重试',
    noDailyUsage: '暂无按日用量数据',
  },

  // Setup Wizard
  setup: {
    title: 'Sub2API 安装向导',
    description: '配置您的 Sub2API 实例',
    database: {
      title: '数据库配置',
      description: '连接到您的 PostgreSQL 数据库',
      host: '主机',
      port: '端口',
      username: '用户名',
      password: '密码',
      databaseName: '数据库名称',
      sslMode: 'SSL 模式',
      passwordPlaceholder: '密码',
      ssl: {
        disable: '禁用',
        require: '要求',
        verifyCa: '验证 CA',
        verifyFull: '完全验证'
      }
    },
    redis: {
      title: 'Redis 配置',
      description: '连接到您的 Redis 服务器',
      host: '主机',
      port: '端口',
      username: '用户名（可选）',
      password: '密码（可选）',
      database: '数据库',
      usernamePlaceholder: '默认用户留空',
      passwordPlaceholder: '密码',
      enableTls: '启用 TLS',
      enableTlsHint: '连接 Redis 时使用 TLS（公共 CA 证书）'
    },
    admin: {
      title: '管理员账户',
      description: '创建您的管理员账户',
      email: '邮箱',
      password: '密码',
      confirmPassword: '确认密码',
      passwordPlaceholder: '至少 8 个字符',
      confirmPasswordPlaceholder: '确认密码',
      passwordMismatch: '密码不匹配'
    },
    ready: {
      title: '准备安装',
      description: '检查您的配置并完成安装',
      database: '数据库',
      redis: 'Redis',
      adminEmail: '管理员邮箱'
    },
    status: {
      testing: '测试中...',
      success: '连接成功',
      testConnection: '测试连接',
      installing: '安装中...',
      completeInstallation: '完成安装',
      completed: '安装完成！',
      redirecting: '正在跳转到登录页面...',
      restarting: '服务正在重启，请稍候...',
      timeout: '服务重启时间超出预期，请手动刷新页面。'
    }
  },

  // Common
}
