export default {
  batchImageGuide: {
    title: 'Batch Image Generation',
    description: 'Submit multiple prompts in one job and download the generated images when complete'
  },
  // Home Page
  home: {
    viewOnGithub: 'View on GitHub',
    viewDocs: 'View Documentation',
    docs: 'Docs',
    switchToLight: 'Switch to Light Mode',
    switchToDark: 'Switch to Dark Mode',
    dashboard: 'Dashboard',
    login: 'Login',
    getStarted: 'Get Started',
    goToDashboard: 'Go to Dashboard',
    // User-focused value proposition
    heroSubtitle: 'One Key, All AI Models',
    heroDescription: 'No need to manage multiple subscriptions. Access Claude, GPT, Gemini and more with a single API key',
    tags: {
      subscriptionToApi: 'Subscription to API',
      stickySession: 'Session Persistence',
      realtimeBilling: 'Pay As You Go'
    },
    // Pain points section
    painPoints: {
      title: 'Sound Familiar?',
      items: {
        expensive: {
          title: 'High Subscription Costs',
          desc: 'Paying for multiple AI subscriptions that add up every month'
        },
        complex: {
          title: 'Account Chaos',
          desc: 'Managing scattered accounts and API keys across different platforms'
        },
        unstable: {
          title: 'Service Interruptions',
          desc: 'Single accounts hitting rate limits and disrupting your workflow'
        },
        noControl: {
          title: 'No Usage Control',
          desc: "Can't track where your money goes or limit team member usage"
        }
      }
    },
    // Solutions section
    solutions: {
      title: 'We Solve These Problems',
      subtitle: 'Three simple steps to stress-free AI access'
    },
    features: {
      unifiedGateway: 'One-Click Access',
      unifiedGatewayDesc: 'Get a single API key to call all connected AI models. No separate applications needed.',
      multiAccount: 'Always Reliable',
      multiAccountDesc: 'Smart routing across multiple upstream accounts with automatic failover. Say goodbye to errors.',
      balanceQuota: 'Pay What You Use',
      balanceQuotaDesc: 'Usage-based billing with quota limits. Full visibility into team consumption.'
    },
    // Comparison section
    comparison: {
      title: 'Why Choose Us?',
      headers: {
        feature: 'Comparison',
        official: 'Official Subscriptions',
        us: 'Our Platform'
      },
      items: {
        pricing: {
          feature: 'Pricing',
          official: 'Fixed monthly fee, pay even if unused',
          us: 'Pay only for what you use'
        },
        models: {
          feature: 'Model Selection',
          official: 'Single provider only',
          us: 'Switch between models freely'
        },
        management: {
          feature: 'Account Management',
          official: 'Manage each service separately',
          us: 'Unified key, one dashboard'
        },
        stability: {
          feature: 'Stability',
          official: 'Single account rate limits',
          us: 'Multi-account pool, auto-failover'
        },
        control: {
          feature: 'Usage Control',
          official: 'Not available',
          us: 'Quotas & detailed analytics'
        }
      }
    },
    providers: {
      title: 'Supported AI Models',
      description: 'One API, Multiple Choices',
      supported: 'Supported',
      soon: 'Soon',
      claude: 'Claude',
      gemini: 'Gemini',
      antigravity: 'Antigravity',
      more: 'More'
    },
    // CTA section
    cta: {
      title: 'Ready to Get Started?',
      description: 'Sign up now and get free trial credits to experience seamless AI access',
      button: 'Sign Up Free'
    },
    footer: {
      allRightsReserved: 'All rights reserved.'
    },
    public: {
    b2: {
      integrationTitle: 'Connect in one minute', integrationDescription: 'Compatible with the OpenAI SDK. Just change the Base URL.',
      fullDocs: 'Read the full docs', codeLanguage: 'Code language', copyCode: 'Copy code', copied: 'Copied', copyFailed: 'Copy failed', contacts: 'Contact'
    },
    nav: {
      apiGateway: 'API gateway', channels: 'Channels', pricing: 'Pricing', modelPlaza: 'Model plaza', docs: 'Docs', startBuilding: 'Start building'
    },
    contact: { joinNow: 'Join the community' },
    hero: {
      eyebrow: 'Unified model gateway', title: 'One gateway. Every model that matters.',
      titleLead: 'One API, ', titleAccent: 'every frontier model, ', titleEnd: 'smart routing.',
      subtitle: 'Route OpenAI-compatible requests across the providers your product depends on, with transparent pricing and operational visibility.',
      createApiKey: 'Create an API key', exploreApi: 'Read the integration guide', gatewayPreview: 'Gateway preview', keysStayYours: 'Keys stay yours', joinGroup: 'Join QQ group',
      channels: 'channels', platforms: 'platforms', protocol: 'protocol', available: '{count} available', signInRequired: 'Sign in required', loading: 'Loading', error: 'Temporarily unavailable', empty: 'No channels yet', notLoaded: 'Not loaded', openAi: 'OpenAI',
      routingLayer: 'Routing layer', smartFallback: 'Smart fallback', visibleChannels: 'Visible channels', supportedModels: 'Supported models', providers: 'Providers', compatibility: 'Compatibility', routing: 'Routing', openAiApi: 'OpenAI API', smartSticky: 'Smart + sticky', p50Latency: 'p50 latency', successRate: 'success rate', activeRoutes: 'active routes', readDocs: 'Read the docs'
    },
    models: { title: '8 leading model providers', pricing: 'View model details', app: 'Your application', appType: 'Web · App · Agent', gateway: 'Unified model access', entry: 'API endpoint', platforms: 'Model providers', coverage: 'Connected to 8 leading model providers and expanding' },
    overview: {
      channelStatus: 'Channel status', available: '{count} available', signInRequired: 'Sign in required', loading: 'Loading', error: 'Temporarily unavailable', empty: 'No channels yet', notLoaded: 'Not loaded', modelsIndexed: 'Models indexed', platforms: 'Platforms', gateway: 'Gateway', openAiCompatible: 'OpenAI-compatible'
    },
    channelStatus: {
      eyebrow: 'Platform coverage', title: 'Channel status', description: 'See which providers and model groups are currently available through your gateway.', platformsAvailable: '{count} platform(s) available',
      loadingAria: 'Loading channel status', errorTitle: 'Channel status is temporarily unavailable', errorFallback: 'Please try again in a moment.', retry: 'Try again',
      unavailableTitle: 'Channel data is unavailable', unavailableDescription: 'Public channel data could not be loaded. Please try again later.',
      idleTitle: 'Channel status has not loaded yet', idleDescription: 'Status data will appear here when the public summary is requested.', emptyTitle: 'No channel data yet', emptyDescription: 'There are no channels available for this workspace yet.',
      active: 'Active', table: { platform: 'Platform', status: 'Status', channels: 'Channels', groups: 'Groups', models: 'Models' }, noNamedChannels: 'No named channels', moreChannels: '+{count} more', namesNote: 'Public group coverage for enabled channels, not a live health check.', viewFullStatus: 'View model details'
    },
    pricing: {
      eyebrow: 'Model access', title: 'Pricing and model coverage', description: 'Browse published model prices by platform. Configured prices are separated from official reference prices when available.', modelsShown: '{shown} of {total} models shown',
      loadingAria: 'Loading pricing summary', errorTitle: 'Pricing data is temporarily unavailable', errorFallback: 'Please try again in a moment.', retry: 'Try again', unavailableTitle: 'Pricing data is unavailable', unavailableDescription: 'Public pricing data could not be loaded. Please try again later.',
      idleTitle: 'Pricing has not loaded yet', idleDescription: 'The published model summary will appear here when requested.', emptyTitle: 'No published models yet', emptyDescription: 'Pricing will appear when the model plaza has entries.', noMatchesTitle: 'No models match these filters', noMatchesDescription: 'Try a different search term or platform.',
      searchPlaceholder: 'Search models', platform: 'Platform', allPlatforms: 'All platforms', clearFilters: 'Clear filters', caption: 'Published model prices by platform', table: { platform: 'Platform', model: 'Model', billing: 'Billing', input: 'Input', output: 'Output', cacheRead: 'Cache read', groups: 'Groups', perToken: 'USD / token' },
      officialReference: 'Official reference', noPricePublished: 'No price published', perRequest: 'Per request', perRequestWithPrice: 'Per request {price}', tokenBilling: 'Per token', reference: 'Reference', notSpecified: 'Not specified', footerNote: 'Prices are provider-configured and may vary by billing mode.', browseModelPlaza: 'Browse model plaza'
    },
    integration: {
      eyebrow: 'Drop-in integration', title: 'Get started in 1 minute', description: 'Use the SDKs you already know. Point them at one OpenAI-compatible base URL, then let {siteName} handle provider routing, retries, and usage metering.',
      benefits: { sdk: 'OpenAI-compatible REST and SDKs', failover: 'Automatic failover across configured channels', limits: 'Per-key limits, logs, and usage visibility' }, apiReference: 'Read the integration guide', codeExamples: 'Code examples', copy: 'Copy', baseUrl: 'Base URL', curl: 'cURL', javascript: 'JavaScript', python: 'Python', codePanelAria: 'Code examples', codePanelFor: '{label} code example'
    },
    footer: {
      builtForTeams: 'Built for teams shipping with AI.',
      brandDescription: 'Unified model access and smart routing',
      contactTitle: 'Contact us',
      serviceLabel: 'Support contact',
      copyContact: 'Copy contact',
      linksLabel: 'Related links',
      serviceStatus: 'Unified API gateway · Ready to connect'
    }
    }
  },

  // Key Usage Query Page
  keyUsage: {
    title: 'API Key Usage',
    subtitle: 'Enter your API Key to view real-time spending and usage status',
    placeholder: 'sk-ant-mirror-xxxxxxxxxxxx',
    query: 'Query',
    querying: 'Querying...',
    privacyNote: 'Your Key is processed locally in the browser and will not be stored',
    dateRange: 'Date Range:',
    dateRangeToday: 'Today',
    dateRange7d: '7 Days',
    dateRange30d: '30 Days',
    dateRange90d: '90 Days',
    dateRangeCustom: 'Custom',
    apply: 'Apply',
    used: 'Used',
    detailInfo: 'Detail Information',
    tokenStats: 'Token Statistics',
    dailyDetail: 'Daily Detail',
    modelStats: 'Model Usage Statistics',
    // Table headers
    date: 'Date',
    model: 'Model',
    requests: 'Requests',
    inputTokens: 'Input Tokens',
    outputTokens: 'Output Tokens',
    cacheCreationTokens: 'Cache Creation',
    cacheReadTokens: 'Cache Read',
    cacheWriteTokens: 'Cache Write',
    totalTokens: 'Total Tokens',
    cost: 'Cost',
    // Status
    quotaMode: 'Key Quota Mode',
    walletBalance: 'Wallet Balance',
    // Ring card titles
    totalQuota: 'Total Quota',
    limit5h: '5-Hour Limit',
    limitDaily: 'Daily Limit',
    limit7d: '7-Day Limit',
    limitWeekly: 'Weekly Limit',
    limitMonthly: 'Monthly Limit',
    // Detail rows
    remainingQuota: 'Remaining Quota',
    expiresAt: 'Expires At',
    todayExpires: '(expires today)',
    daysLeft: '({days} days)',
    usedQuota: 'Used Quota',
    resetNow: 'Resetting soon',
    subscriptionType: 'Subscription Type',
    billingType: 'Billing Type',
    subscriptionExpires: 'Subscription Expires',
    // Usage stat cells
    todayRequests: 'Today Requests',
    todayInputTokens: 'Today Input',
    todayOutputTokens: 'Today Output',
    todayTokens: 'Today Tokens',
    todayCacheCreation: 'Today Cache Creation',
    todayCacheRead: 'Today Cache Read',
    todayCost: 'Today Cost',
    rpmTpm: 'RPM / TPM',
    totalRequests: 'Total Requests',
    totalInputTokens: 'Total Input',
    totalOutputTokens: 'Total Output',
    totalTokensLabel: 'Total Tokens',
    totalCacheCreation: 'Total Cache Creation',
    totalCacheRead: 'Total Cache Read',
    totalCost: 'Total Cost',
    avgDuration: 'Avg Duration',
    // Messages
    enterApiKey: 'Please enter an API Key',
    querySuccess: 'Query successful',
    queryFailed: 'Query failed',
    queryFailedRetry: 'Query failed, please try again later',
    noDailyUsage: 'No daily usage data',
  },

  // Setup Wizard
  setup: {
    title: 'Sub2API Setup',
    description: 'Configure your Sub2API instance',
    database: {
      title: 'Database Configuration',
      description: 'Connect to your PostgreSQL database',
      host: 'Host',
      port: 'Port',
      username: 'Username',
      password: 'Password',
      databaseName: 'Database Name',
      sslMode: 'SSL Mode',
      passwordPlaceholder: 'Password',
      ssl: {
        disable: 'Disable',
        require: 'Require',
        verifyCa: 'Verify CA',
        verifyFull: 'Verify Full'
      }
    },
    redis: {
      title: 'Redis Configuration',
      description: 'Connect to your Redis server',
      host: 'Host',
      port: 'Port',
      username: 'Username (optional)',
      password: 'Password (optional)',
      database: 'Database',
      usernamePlaceholder: 'Leave empty for default user',
      passwordPlaceholder: 'Password',
      enableTls: 'Enable TLS',
      enableTlsHint: 'Use TLS when connecting to Redis (public CA certs)'
    },
    admin: {
      title: 'Admin Account',
      description: 'Create your administrator account',
      email: 'Email',
      password: 'Password',
      confirmPassword: 'Confirm Password',
      passwordPlaceholder: 'Min 8 characters',
      confirmPasswordPlaceholder: 'Confirm password',
      passwordMismatch: 'Passwords do not match'
    },
    ready: {
      title: 'Ready to Install',
      description: 'Review your configuration and complete setup',
      database: 'Database',
      redis: 'Redis',
      adminEmail: 'Admin Email'
    },
    status: {
      testing: 'Testing...',
      success: 'Connection Successful',
      testConnection: 'Test Connection',
      installing: 'Installing...',
      completeInstallation: 'Complete Installation',
      completed: 'Installation completed!',
      redirecting: 'Redirecting to login page...',
      restarting: 'Service is restarting, please wait...',
      timeout: 'Service restart is taking longer than expected. Please refresh the page manually.'
    }
  },

  // Common
}
