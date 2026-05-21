// Typed localization tables for the Trine Signer sample app. Visible chrome
// (titles, section headings, buttons, empty states, settings labels,
// onboarding copy) lives here in both Chinese and English. Demo content
// (wallet monikers, addresses, audit records) intentionally stays as
// per-record strings: it represents user-entered data, not chrome.

export const LOCALES = ['zh', 'en'] as const;
export type Locale = (typeof LOCALES)[number];

export interface Strings {
  readonly app: {
    readonly name: string;
    readonly initializing: string;
    readonly initFailed: string;
  };
  readonly tabs: {
    readonly inbox: string;
    readonly wallets: string;
    readonly audit: string;
    readonly settings: string;
  };
  readonly common: {
    readonly continue: string;
    readonly back: string;
    readonly approve: string;
    readonly reject: string;
    readonly cancel: string;
    readonly add: string;
    readonly retry: string;
    readonly pleaseWait: string;
    readonly stepOf: string;
    readonly justNow: string;
  };
  readonly inbox: {
    readonly pendingTitle: string;
    readonly meta: string;
    readonly filterAll: string;
    readonly filterMine: string;
    readonly filterSuspicious: string;
    readonly needsDecision: string;
    readonly decided: string;
    readonly approvedOf: string;
    readonly mismatchFallback: string;
    readonly expired: string;
    readonly emptyTitle: string;
    readonly emptySub: string;
  };
  readonly wallets: {
    readonly title: string;
    readonly addresses: string;
    readonly thisDevice: string;
    readonly online: string;
    readonly offline: string;
    readonly standby: string;
    readonly newWallet: string;
    readonly newWalletSub: string;
  };
  readonly audit: {
    readonly title: string;
    readonly signed: string;
    readonly rejected: string;
    readonly expired: string;
  };
  readonly settings: {
    readonly title: string;
    readonly memberSince: string;
    readonly memberOf: string;
    readonly totalParties: string;
    readonly languageLabel: string;
    readonly languageEn: string;
    readonly languageZh: string;
    readonly themeLabel: string;
    readonly themeMidnight: string;
    readonly themeOnyx: string;
    readonly sections: {
      readonly identity: string;
      readonly connection: string;
      readonly shares: string;
      readonly security: string;
      readonly setup: string;
      readonly appearance: string;
    };
    readonly rows: {
      readonly memberId: string;
      readonly device: string;
      readonly identityPub: string;
      readonly identityPubSub: string;
      readonly coordEndpoint: string;
      readonly coordSub: string;
      readonly relayPeer: string;
      readonly dispatch: string;
      readonly dispatchValue: string;
      readonly dispatchSub: string;
      readonly exportShare: string;
      readonly exportShareSub: string;
      readonly importShare: string;
      readonly changeKek: string;
      readonly reshare: string;
      readonly reshareSub: string;
      readonly keygen: string;
      readonly keygenSub: string;
      readonly faceId: string;
      readonly faceIdValue: string;
      readonly zeroize: string;
      readonly zeroizeValue: string;
      readonly strictWysiwys: string;
      readonly strictWysiwysValue: string;
      readonly strictWysiwysSub: string;
      readonly rerunOnboarding: string;
      readonly rerunOnboardingSub: string;
      readonly attestLog: string;
      readonly diagnostics: string;
      readonly about: string;
      readonly aboutValue: string;
    };
  };
  readonly onboarding: {
    readonly identityTitle: string;
    readonly identitySubtitle: string;
    readonly passphrasePrimary: string;
    readonly passphraseLabel: string;
    readonly passphrasePlaceholder: string;
    readonly passphraseRepeat: string;
    readonly passphraseMismatch: string;
    readonly passphraseNoteTitle: string;
    readonly passphraseNoteBody: string;
    readonly passphraseNoteBold: string;
    readonly biometrics: string;
    readonly faceIdUnlock: string;
    readonly faceIdSub: string;
    readonly coordTitle: string;
    readonly coordSubtitle: string;
    readonly cameraHint: string;
    readonly fromAlbum: string;
    readonly enterManually: string;
    readonly bootstrapNote: string;
    readonly confirmTitle: string;
    readonly confirmSubtitle: string;
    readonly confirmPrimary: string;
    readonly sectionCoord: string;
    readonly sectionRelay: string;
    readonly sectionBootstrap: string;
    readonly httpEndpoint: string;
    readonly authentication: string;
    readonly authValue: string;
    readonly peerId: string;
    readonly multiaddrs: string;
    readonly oneTimeToken: string;
    readonly expires: string;
    readonly targetWallet: string;
    readonly targetWalletSub: string;
    readonly verifyWarn: string;
    readonly verifyWarnBold: string;
    readonly registerTitle: string;
    readonly registerSubtitle: string;
    readonly httpHeading: string;
    readonly stageDerive: string;
    readonly stageDeriveSub: string;
    readonly stagePost: string;
    readonly stagePostSub: string;
    readonly stageVerify: string;
    readonly stageVerifySub: string;
    readonly stageAssign: string;
    readonly stageAssignSub: string;
    readonly doneTitle: string;
    readonly doneSubtitle: string;
    readonly doneEnterInbox: string;
    readonly memberIdLabel: string;
    readonly partyIndex: string;
    readonly coordLabel: string;
    readonly coordValueSub: string;
    readonly relayLabel: string;
    readonly relayValueSub: string;
  };
  readonly sdkPanel: {
    readonly title: string;
    readonly activeFlow: string;
    readonly keygen: string;
    readonly sign: string;
    readonly reshare: string;
  };
  readonly signing: {
    readonly detailTitle: string;
    readonly proposer: string;
    readonly receivedAt: string;
    readonly aZoneHeading: string;
    readonly aZoneSub: string;
    readonly bZoneHeading: string;
    readonly bZoneSub: string;
    readonly checksHeading: string;
    readonly metaHashOk: string;
    readonly metaHashBad: string;
    readonly crossCheckOk: string;
    readonly crossCheckBad: string;
    readonly digestLabel: string;
    readonly digestSub: string;
    readonly toLabel: string;
    readonly toLabelSafe: string;
    readonly toLabelDanger: string;
    readonly valueLabel: string;
    readonly fiatLabel: string;
    readonly nonceLabel: string;
    readonly gasLimitLabel: string;
    readonly gasPriceLabel: string;
    readonly dataLabel: string;
    readonly emptyData: string;
    readonly orderLabel: string;
    readonly operatorLabel: string;
    readonly memoLabel: string;
    readonly walletLabel: string;
    readonly quorumHeading: string;
    readonly quorumSub: string;
    readonly memberThis: string;
    readonly memberApproved: string;
    readonly memberRejected: string;
    readonly memberPending: string;
    readonly mismatchTitle: string;
    readonly mismatchBody: string;
    readonly mismatchForceReject: string;
    readonly approveCta: string;
    readonly rejectCta: string;
    readonly progressTitle: string;
    readonly progressSub: string;
    readonly stagePrepare: string;
    readonly stagePrepareSub: string;
    readonly stageRound1: string;
    readonly stageRound1Sub: string;
    readonly stageRound2: string;
    readonly stageRound2Sub: string;
    readonly stageRound3: string;
    readonly stageRound3Sub: string;
    readonly stageCombine: string;
    readonly stageCombineSub: string;
    readonly stageReport: string;
    readonly stageReportSub: string;
    readonly stageBadgeActive: string;
    readonly stageBadgeDone: string;
    readonly stageBadgeWait: string;
    readonly resultSignedTitle: string;
    readonly resultSignedSub: string;
    readonly resultRejectedTitle: string;
    readonly resultRejectedSub: string;
    readonly resultExpiredTitle: string;
    readonly resultExpiredSub: string;
    readonly resultRsvLabel: string;
    readonly resultReportLabel: string;
    readonly resultBackToInbox: string;
    readonly cancelFlow: string;
    readonly demoBannerLive: string;
    readonly demoBannerDemo: string;
  };
}

const en: Strings = {
  app: {
    name: 'Trine Signer',
    initializing: 'Initializing device keystore…',
    initFailed: 'SDK init failed',
  },
  tabs: {
    inbox: 'Inbox',
    wallets: 'Wallets',
    audit: 'Audit',
    settings: 'Settings',
  },
  common: {
    continue: 'Continue',
    back: 'Back',
    approve: 'Approve',
    reject: 'Reject',
    cancel: 'Cancel',
    add: 'Add',
    retry: 'Retry',
    pleaseWait: 'Please wait…',
    stepOf: 'Step {step} of {total}',
    justNow: 'just now',
  },
  inbox: {
    pendingTitle: 'pending signing requests',
    meta: 'identity {memberId} · relay {relay}',
    filterAll: 'All · {count}',
    filterMine: 'Need me · {count}',
    filterSuspicious: 'Suspicious · {count}',
    needsDecision: 'Needs your decision',
    decided: 'Decided',
    approvedOf: '{approved} / {threshold} approved',
    mismatchFallback: 'A facts / B info mismatch',
    expired: 'expired',
    emptyTitle: 'No pending requests',
    emptySub: 'New requests stream in from coord via long-poll and webhook fallback.',
  },
  wallets: {
    title: 'Wallets',
    addresses: 'addresses',
    thisDevice: 'this device',
    online: 'online',
    offline: 'offline',
    standby: 'standby',
    newWallet: 'New wallet · start keygen',
    newWalletSub: 'Receive DKG config from coord START',
  },
  audit: {
    title: 'Audit log',
    signed: 'Signed',
    rejected: 'Rejected',
    expired: 'Expired',
  },
  settings: {
    title: 'Settings',
    memberSince: 'Member of',
    memberOf: 'Member of {count} wallets',
    totalParties: 'total parties {count}',
    languageLabel: 'Language',
    languageEn: 'English',
    languageZh: '中文',
    themeLabel: 'Theme',
    themeMidnight: 'Midnight',
    themeOnyx: 'Onyx',
    sections: {
      identity: 'Identity',
      connection: 'Connection',
      shares: 'Shares',
      security: 'Security',
      setup: 'Setup',
      appearance: 'Appearance',
    },
    rows: {
      memberId: 'memberId',
      device: 'Device',
      identityPub: 'Identity public key',
      identityPubSub: 'Only this device holds the private half',
      coordEndpoint: 'Coord endpoint',
      coordSub: '{tls} · last heartbeat {when}',
      relayPeer: 'Relay peer',
      dispatch: 'B6 dispatch',
      dispatchValue: 'long-poll',
      dispatchSub: 'Webhook sidecar also enabled',
      exportShare: 'Export share backup',
      exportShareSub: 'Argon2id-wrapped, never plain text',
      importShare: 'Import share backup',
      changeKek: 'Change keystore passphrase',
      reshare: 'Reshare',
      reshareSub: 'Rotate shares while keeping the group public key',
      keygen: 'New wallet (keygen)',
      keygenSub: 'Start a new t-of-n DKG run',
      faceId: 'Face ID approval',
      faceIdValue: 'On',
      zeroize: 'Idle zeroize timeout',
      zeroizeValue: '5 minutes',
      strictWysiwys: 'WYSIWYS strict mode',
      strictWysiwysValue: 'On',
      strictWysiwysSub: 'Refuse to sign when A facts and B info disagree',
      rerunOnboarding: 'Re-run onboarding',
      rerunOnboardingSub: 'Rebind identity / coord endpoint',
      attestLog: 'View attestation log',
      diagnostics: 'Diagnostics · export logs',
      about: 'About',
      aboutValue: 'v1.0.0 (P4)',
    },
  },
  onboarding: {
    identityTitle: 'Generate device identity',
    identitySubtitle:
      'Create a secp256k1 identity key pair for this device. The private half is wrapped by your keystore passphrase and never leaves the device.',
    passphrasePrimary: 'Generate identity key',
    passphraseLabel: 'Keystore passphrase',
    passphrasePlaceholder: 'At least 8 characters · 12+ recommended',
    passphraseRepeat: 'Repeat passphrase',
    passphraseMismatch: 'Passphrases do not match',
    passphraseNoteTitle: 'This passphrase matters',
    passphraseNoteBody:
      'Every share and the identity private key are wrapped with a KEK derived from this passphrase (Argon2id).',
    passphraseNoteBold: 'Lose the passphrase and this device can no longer sign',
    biometrics: 'Biometrics',
    faceIdUnlock: 'Face ID unlock',
    faceIdSub: 'Required on each approval, app start, and share unwrap',
    coordTitle: 'Scan onboarding QR',
    coordSubtitle:
      'Ask your operator for the access QR — it carries the coord endpoint, relay multiaddrs, and a one-time bootstrap token.',
    cameraHint: 'Place QR inside the frame',
    fromAlbum: 'From album',
    enterManually: 'Enter manually',
    bootstrapNote:
      'The bootstrap token inside the QR is single-use (default 10 minutes). It is bound to this device’s identity public key and invalidates on registration.',
    confirmTitle: 'Confirm endpoints',
    confirmSubtitle:
      'Make sure these values are from a trusted source. Once you continue, your identity public key is uploaded to that coord.',
    confirmPrimary: 'Confirm and register with coord',
    sectionCoord: 'Coord server',
    sectionRelay: 'Relay network',
    sectionBootstrap: 'Bootstrap',
    httpEndpoint: 'HTTP endpoint',
    authentication: 'Authentication',
    authValue: 'mTLS + client certificate',
    peerId: 'peerID',
    multiaddrs: 'multiaddrs',
    oneTimeToken: 'One-time token',
    expires: 'Expires',
    targetWallet: 'Target wallet',
    targetWalletSub: 'Receives START dispatch after registration',
    verifyWarn:
      'the coord domain and certificate SHA256. Connecting to the wrong coord lets an attacker harvest your identity public key — they cannot sign for you but they can consume your quota.',
    verifyWarnBold: 'Verify with your operator',
    registerTitle: 'Register public key with coord',
    registerSubtitle: 'Uploads only the identity public key. The private half stays on this device.',
    httpHeading: 'HTTP REQUEST',
    stageDerive: 'Derive identity public key',
    stageDeriveSub: 'secp256k1 · 0x04a2c9f1…8e3b',
    stagePost: 'POST /v1/members/enroll',
    stagePostSub: 'Authorization: Bearer btk_2J9XaQ…',
    stageVerify: 'Coord verifies signature and bootstrap',
    stageVerifySub: 'expected_members updated · sqlite write',
    stageAssign: 'Assign memberId · sync group config',
    stageAssignSub: 'memberId = m0 · partyIndex = 0',
    doneTitle: 'Coord enrolled',
    doneSubtitle:
      'Public key registered, identity private key stays on this device. Wait for a keygen START dispatch from coord.',
    doneEnterInbox: 'Enter inbox',
    memberIdLabel: 'memberId',
    partyIndex: 'partyIndex',
    coordLabel: 'coord',
    coordValueSub: 'mTLS · 38ms',
    relayLabel: 'relay',
    relayValueSub: 'libp2p stream established',
  },
  sdkPanel: {
    title: 'SDK developer panel',
    activeFlow: 'ACTIVE FLOW: {flow}',
    keygen: 'Keygen',
    sign: 'Sign',
    reshare: 'Reshare',
  },
  signing: {
    detailTitle: 'Review and sign',
    proposer: 'Proposer',
    receivedAt: 'Received',
    aZoneHeading: 'A · Verified facts',
    aZoneSub: 'Re-derived locally from unsignedTx, the sole funds-safety authority',
    bZoneHeading: 'B · Business info',
    bZoneSub: 'Advisory only — supplied by the proposer, never authoritative',
    checksHeading: 'Integrity checks',
    metaHashOk: 'metaHash matches proposer signature',
    metaHashBad: 'metaHash does NOT match proposer signature',
    crossCheckOk: 'A facts cross-check B info',
    crossCheckBad: 'A facts and B info disagree',
    digestLabel: 'digest32',
    digestSub: 'Re-derived locally and bound 1:1 with unsignedTx',
    toLabel: 'To',
    toLabelSafe: 'allow-listed',
    toLabelDanger: 'NOT on allow-list',
    valueLabel: 'Value',
    fiatLabel: 'Estimate',
    nonceLabel: 'Nonce',
    gasLimitLabel: 'Gas limit',
    gasPriceLabel: 'Gas price',
    dataLabel: 'calldata',
    emptyData: '0x · plain transfer',
    orderLabel: 'Order ID',
    operatorLabel: 'Operator',
    memoLabel: 'Memo',
    walletLabel: 'Wallet',
    quorumHeading: 'Quorum decisions',
    quorumSub: '{approved} of {threshold} approvals collected · {parties} parties',
    memberThis: 'this device',
    memberApproved: 'approved',
    memberRejected: 'rejected',
    memberPending: 'awaiting',
    mismatchTitle: 'A / B mismatch · approval disabled',
    mismatchBody:
      'The proposer’s business info does not match the re-derived facts. WYSIWYS strict mode refuses to sign — please reject this request.',
    mismatchForceReject: 'Reject (recommended)',
    approveCta: 'Approve and sign',
    rejectCta: 'Reject',
    progressTitle: 'Signing in progress',
    progressSub: 'MPC round running across {parties} parties · digest {digest}',
    stagePrepare: 'Prepare session',
    stagePrepareSub: 'Pin digest32 · open libp2p stream to signers',
    stageRound1: 'Round 1 · commitments',
    stageRound1Sub: 'Exchange paillier and feldman commitments',
    stageRound2: 'Round 2 · shares',
    stageRound2Sub: 'Distribute and verify share contributions',
    stageRound3: 'Round 3 · sign',
    stageRound3Sub: 'Compute partial sigma_i across all signers',
    stageCombine: 'Combine · {R,S,V}',
    stageCombineSub: 'Aggregate to 65-byte secp256k1 signature',
    stageReport: 'Report to coord',
    stageReportSub: 'POST /v1/requests/{id}/sign · attestation log',
    stageBadgeActive: 'running',
    stageBadgeDone: 'done',
    stageBadgeWait: 'queued',
    resultSignedTitle: 'Signed',
    resultSignedSub: 'Signature accepted by coord and broadcast queued',
    resultRejectedTitle: 'Rejected',
    resultRejectedSub: 'No share material was used — MPC never started',
    resultExpiredTitle: 'Expired',
    resultExpiredSub: 'Decision window closed before approval',
    resultRsvLabel: '{R,S,V} · 65 bytes',
    resultReportLabel: 'Reason',
    resultBackToInbox: 'Back to inbox',
    cancelFlow: 'Cancel',
    demoBannerLive: 'Live SignSession',
    demoBannerDemo: 'Demo SignSession · approve/reject still preserve bridge shape',
  },
};

const zh: Strings = {
  app: {
    name: 'Trine Signer',
    initializing: '正在初始化设备 keystore…',
    initFailed: 'SDK 初始化失败',
  },
  tabs: {
    inbox: '请求',
    wallets: '钱包',
    audit: '审计',
    settings: '设置',
  },
  common: {
    continue: '继续',
    back: '返回',
    approve: '批准',
    reject: '拒绝',
    cancel: '取消',
    add: '添加',
    retry: '重试',
    pleaseWait: '请稍候…',
    stepOf: '第 {step} / {total} 步',
    justNow: '刚刚',
  },
  inbox: {
    pendingTitle: '个待签名请求',
    meta: '身份 {memberId} · relay {relay}',
    filterAll: '全部 · {count}',
    filterMine: '需我决策 · {count}',
    filterSuspicious: '可疑 · {count}',
    needsDecision: '需要我决策',
    decided: '已决策',
    approvedOf: '{approved} / {threshold} 已批',
    mismatchFallback: 'A facts / B info 不匹配',
    expired: '已过期',
    emptyTitle: '暂无待签请求',
    emptySub: '我们会经由长轮询从 coord 拉取,新请求会推送到此。',
  },
  wallets: {
    title: '钱包',
    addresses: '个地址',
    thisDevice: '本机',
    online: '在线',
    offline: '离线',
    standby: '待机',
    newWallet: '新建钱包 · 启动 keygen',
    newWalletSub: '从 coord START 接收 DKG 配置',
  },
  audit: {
    title: '审计日志',
    signed: '已签名',
    rejected: '已拒绝',
    expired: '已过期',
  },
  settings: {
    title: '设置',
    memberSince: '已加入',
    memberOf: '加入 {count} 个钱包',
    totalParties: '总参与方 {count}',
    languageLabel: '语言',
    languageEn: 'English',
    languageZh: '中文',
    themeLabel: '主题',
    themeMidnight: '午夜',
    themeOnyx: '黑曜',
    sections: {
      identity: '身份',
      connection: '连接',
      shares: '分片',
      security: '安全',
      setup: '初始化',
      appearance: '外观',
    },
    rows: {
      memberId: 'memberId',
      device: '设备',
      identityPub: '身份公钥',
      identityPubSub: '仅本设备持有私钥',
      coordEndpoint: 'coord 端点',
      coordSub: '{tls} · 上次心跳 {when}',
      relayPeer: 'relay peer',
      dispatch: 'B6 调度',
      dispatchValue: '长轮询',
      dispatchSub: '同时启用 webhook 旁路',
      exportShare: '导出分片备份',
      exportShareSub: '口令封装 (Argon2id),绝不明文',
      importShare: '导入分片备份',
      changeKek: '更改 keystore 口令',
      reshare: '重分片 (reshare)',
      reshareSub: '轮换分片,保持组公钥不变',
      keygen: '新建钱包 (keygen)',
      keygenSub: '启动一次 t-of-n DKG',
      faceId: 'Face ID 解锁审批',
      faceIdValue: '已开启',
      zeroize: '审批超时 zeroize',
      zeroizeValue: '5 分钟',
      strictWysiwys: 'WYSIWYS 严格模式',
      strictWysiwysValue: '已开启',
      strictWysiwysSub: 'A facts / B info 不匹配时拒绝签名',
      rerunOnboarding: '重新跑初始化',
      rerunOnboardingSub: '重新绑定身份 / coord 端点',
      attestLog: '查看 attestation 记录',
      diagnostics: '诊断 · 日志导出',
      about: '关于',
      aboutValue: 'v1.0.0 (P4)',
    },
  },
  onboarding: {
    identityTitle: '生成本机身份',
    identitySubtitle:
      '为这台设备生成一对 secp256k1 身份密钥。私钥由 keystore 口令包装,永不离开设备。',
    passphrasePrimary: '生成身份密钥',
    passphraseLabel: 'keystore 口令',
    passphrasePlaceholder: '至少 8 位 · 推荐 12+',
    passphraseRepeat: '再次输入口令',
    passphraseMismatch: '两次输入不一致',
    passphraseNoteTitle: '这把口令很重要',
    passphraseNoteBody:
      '所有的 share 和身份私钥都用这把口令派生的 KEK 封装 (Argon2id)。',
    passphraseNoteBold: '口令丢失 = 此设备无法再参与签名',
    biometrics: '生物识别',
    faceIdUnlock: 'Face ID 解锁',
    faceIdSub: '每次审批 · 启动 · 解封 share 时校验',
    coordTitle: '扫描接入二维码',
    coordSubtitle:
      '向运维要一张接入二维码,包含 coord 端点、relay 多址、一次性 bootstrap token。',
    cameraHint: '将二维码置于框内',
    fromAlbum: '从相册选择',
    enterManually: '手动输入',
    bootstrapNote:
      'QR 中的 bootstrap token 为一次性凭证(默认 10 分钟过期),与本机身份私钥绑定后失效。',
    confirmTitle: '确认接入端点',
    confirmSubtitle:
      '请确认以下信息来源可信。一旦继续,身份公钥即上传给该 coord。',
    confirmPrimary: '确认并注册到 coord',
    sectionCoord: 'coord 服务器',
    sectionRelay: 'relay 网络',
    sectionBootstrap: 'bootstrap',
    httpEndpoint: 'HTTP 端点',
    authentication: '鉴权',
    authValue: 'mTLS + 客户端证书',
    peerId: 'peerID',
    multiaddrs: 'multiaddrs',
    oneTimeToken: '一次性 token',
    expires: '过期时间',
    targetWallet: '目标钱包',
    targetWalletSub: '注册成功后接收 START dispatch',
    verifyWarn:
      ' coord 域名与证书 SHA256 — 接入到错误的 coord 会让攻击者拿到你的身份公钥(无法签名但能消耗配额)。',
    verifyWarnBold: '请向你的运维方核对',
    registerTitle: '向 coord 注册公钥',
    registerSubtitle: '把本机身份公钥上传到 coord。私钥永不离开设备。',
    httpHeading: 'HTTP 请求',
    stageDerive: '派生身份公钥',
    stageDeriveSub: 'secp256k1 · 0x04a2c9f1…8e3b',
    stagePost: 'POST /v1/members/enroll',
    stagePostSub: 'Authorization: Bearer btk_2J9XaQ…',
    stageVerify: 'coord 验签 + 校验 bootstrap',
    stageVerifySub: 'expected_members 添加 · 落 sqlite',
    stageAssign: '分配 memberId · 同步 group config',
    stageAssignSub: 'memberId = m0 · partyIndex = 0',
    doneTitle: '已接入 coord',
    doneSubtitle:
      '公钥已注册,身份私钥安全留在本机。请等待 coord 推送 keygen START dispatch。',
    doneEnterInbox: '进入收件箱',
    memberIdLabel: 'memberId',
    partyIndex: 'partyIndex',
    coordLabel: 'coord',
    coordValueSub: 'mTLS · 38ms',
    relayLabel: 'relay',
    relayValueSub: '已建立 libp2p stream',
  },
  sdkPanel: {
    title: 'SDK 开发面板',
    activeFlow: '当前流程: {flow}',
    keygen: 'Keygen',
    sign: '签名',
    reshare: '重分片',
  },
  signing: {
    detailTitle: '审核并签名',
    proposer: '提案方',
    receivedAt: '收到',
    aZoneHeading: 'A · 已校验事实',
    aZoneSub: '由 unsignedTx 在本机重算,资金安全的唯一权威',
    bZoneHeading: 'B · 业务信息',
    bZoneSub: '仅作参考 — 由提案方提供,绝非权威',
    checksHeading: '完整性核对',
    metaHashOk: 'metaHash 与提案方签名一致',
    metaHashBad: 'metaHash 与提案方签名不一致',
    crossCheckOk: 'A 区事实与 B 区信息一致',
    crossCheckBad: 'A 区事实与 B 区信息不一致',
    digestLabel: 'digest32',
    digestSub: '本机重算,与 unsignedTx 1:1 绑定',
    toLabel: '收款方',
    toLabelSafe: '在白名单',
    toLabelDanger: '不在白名单',
    valueLabel: '金额',
    fiatLabel: '估值',
    nonceLabel: 'Nonce',
    gasLimitLabel: 'Gas 上限',
    gasPriceLabel: 'Gas 价格',
    dataLabel: 'calldata',
    emptyData: '0x · 纯转账',
    orderLabel: '工单号',
    operatorLabel: '操作员',
    memoLabel: '备注',
    walletLabel: '钱包',
    quorumHeading: '法定人数决策',
    quorumSub: '已收集 {approved} / {threshold} 批准 · 共 {parties} 方',
    memberThis: '本机',
    memberApproved: '已批准',
    memberRejected: '已拒绝',
    memberPending: '等待中',
    mismatchTitle: 'A / B 不匹配 · 已禁用批准',
    mismatchBody:
      '提案方的业务信息与本机重算事实不一致。WYSIWYS 严格模式拒绝签名 — 请拒绝该请求。',
    mismatchForceReject: '拒绝 (建议)',
    approveCta: '批准并签名',
    rejectCta: '拒绝',
    progressTitle: '正在签名',
    progressSub: '{parties} 方协同 MPC · digest {digest}',
    stagePrepare: '初始化会话',
    stagePrepareSub: '锁定 digest32 · 建立 libp2p 流',
    stageRound1: '第 1 轮 · 承诺',
    stageRound1Sub: '交换 paillier / feldman 承诺',
    stageRound2: '第 2 轮 · 份额',
    stageRound2Sub: '分发并校验份额贡献',
    stageRound3: '第 3 轮 · 签名',
    stageRound3Sub: '各方计算偏 sigma_i',
    stageCombine: '聚合 · {R,S,V}',
    stageCombineSub: '聚合为 65 字节 secp256k1 签名',
    stageReport: '上报 coord',
    stageReportSub: 'POST /v1/requests/{id}/sign · attestation 记录',
    stageBadgeActive: '进行中',
    stageBadgeDone: '完成',
    stageBadgeWait: '等待',
    resultSignedTitle: '已签名',
    resultSignedSub: '签名已被 coord 接受,等待广播',
    resultRejectedTitle: '已拒绝',
    resultRejectedSub: '本机未使用任何分片 — 未进入 MPC',
    resultExpiredTitle: '已过期',
    resultExpiredSub: '决策窗口已关闭',
    resultRsvLabel: '{R,S,V} · 65 字节',
    resultReportLabel: '原因',
    resultBackToInbox: '返回收件箱',
    cancelFlow: '取消',
    demoBannerLive: '已绑定 SignSession',
    demoBannerDemo: '演示 SignSession · approve/reject 保留 bridge 接口形状',
  },
};

export const STRINGS: Record<Locale, Strings> = { en, zh };

export function format(template: string, params: Readonly<Record<string, string | number>>): string {
  return Object.keys(params).reduce(
    (acc, key) => acc.replaceAll(`{${key}}`, String(params[key])),
    template,
  );
}
