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
  readonly walletDetail: {
    readonly title: string;
    readonly defaultBadge: string;
    readonly xpubReady: string;
    readonly xpubPending: string;
    readonly thresholdHeading: string;
    readonly thresholdSub: string;
    readonly membersHeading: string;
    readonly membersSub: string;
    readonly addressesHeading: string;
    readonly addressesSub: string;
    readonly keyMaterialHeading: string;
    readonly keyMaterialSub: string;
    readonly groupIdLabel: string;
    readonly ecdsaPubkeyLabel: string;
    readonly chaincodeLabel: string;
    readonly pathLabel: string;
    readonly deriveAction: string;
    readonly deriveActionSub: string;
    readonly reshareAction: string;
    readonly reshareActionSub: string;
    readonly addressCount: string;
    readonly memberSelfBadge: string;
    readonly memberLastSeen: string;
  };
  readonly derive: {
    readonly title: string;
    readonly subtitle: string;
    readonly chainSection: string;
    readonly pathSection: string;
    readonly pathSub: string;
    readonly previewSection: string;
    readonly previewSub: string;
    readonly previewPlaceholder: string;
    readonly addressLabel: string;
    readonly addAction: string;
    readonly addedTitle: string;
    readonly addedSub: string;
    readonly closeAction: string;
    readonly demoNote: string;
    readonly duplicateNote: string;
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
  readonly keygen: {
    readonly title: string;
    readonly intro: string;
    readonly stepConfigure: string;
    readonly stepConfigureSub: string;
    readonly stepConfirm: string;
    readonly stepConfirmSub: string;
    readonly stepProgress: string;
    readonly stepProgressSub: string;
    readonly stepDone: string;
    readonly stepError: string;
    readonly thresholdLabel: string;
    readonly thresholdSub: string;
    readonly partiesLabel: string;
    readonly partiesSub: string;
    readonly passphraseLabel: string;
    readonly passphrasePlaceholder: string;
    readonly passphraseSub: string;
    readonly quorumPreview: string;
    readonly factCallback: string;
    readonly factCallbackSub: string;
    readonly factGroupPub: string;
    readonly factGroupPubSub: string;
    readonly factShareLocal: string;
    readonly factShareLocalSub: string;
    readonly factCommittee: string;
    readonly factCommitteeSub: string;
    readonly callShape: string;
    readonly callShapeValue: string;
    readonly stageHandshake: string;
    readonly stageHandshakeSub: string;
    readonly stageCommit: string;
    readonly stageCommitSub: string;
    readonly stageShare: string;
    readonly stageShareSub: string;
    readonly stageFinalize: string;
    readonly stageFinalizeSub: string;
    readonly stagePublish: string;
    readonly stagePublishSub: string;
    readonly stageActive: string;
    readonly stageDone: string;
    readonly stageWait: string;
    readonly progressEventLabel: string;
    readonly resultTitle: string;
    readonly resultSub: string;
    readonly resultGroupPubKey: string;
    readonly resultThreshold: string;
    readonly resultParties: string;
    readonly resultMonikers: string;
    readonly resultMonikersFallback: string;
    readonly errorTitle: string;
    readonly errorLaunch: string;
    readonly bannerLive: string;
    readonly bannerDemo: string;
    readonly ctaContinue: string;
    readonly ctaStart: string;
    readonly ctaCancel: string;
    readonly ctaClose: string;
    readonly ctaRetry: string;
  };
  readonly reshare: {
    readonly title: string;
    readonly intro: string;
    readonly invariantTitle: string;
    readonly invariantBody: string;
    readonly stepIntro: string;
    readonly stepIntroSub: string;
    readonly stepConfigure: string;
    readonly stepConfigureSub: string;
    readonly stepConfirm: string;
    readonly stepConfirmSub: string;
    readonly stepProgress: string;
    readonly stepProgressSub: string;
    readonly stepDone: string;
    readonly stepError: string;
    readonly oldThresholdLabel: string;
    readonly oldThresholdSub: string;
    readonly newThresholdLabel: string;
    readonly newThresholdSub: string;
    readonly newPartiesLabel: string;
    readonly newPartiesSub: string;
    readonly passphraseLabel: string;
    readonly passphrasePlaceholder: string;
    readonly passphraseSub: string;
    readonly summaryHeading: string;
    readonly oldQuorum: string;
    readonly newQuorum: string;
    readonly factInvariantPub: string;
    readonly factInvariantPubSub: string;
    readonly factInvariantAddr: string;
    readonly factInvariantAddrSub: string;
    readonly factRotate: string;
    readonly factRotateSub: string;
    readonly factOldWipe: string;
    readonly factOldWipeSub: string;
    readonly callShape: string;
    readonly callShapeValue: string;
    readonly stagePrepare: string;
    readonly stagePrepareSub: string;
    readonly stageDistribute: string;
    readonly stageDistributeSub: string;
    readonly stageRecombine: string;
    readonly stageRecombineSub: string;
    readonly stageInstall: string;
    readonly stageInstallSub: string;
    readonly stageVerify: string;
    readonly stageVerifySub: string;
    readonly stageActive: string;
    readonly stageDone: string;
    readonly stageWait: string;
    readonly progressEventLabel: string;
    readonly resultTitle: string;
    readonly resultSub: string;
    readonly resultGroupPubKey: string;
    readonly resultOldThreshold: string;
    readonly resultNewThreshold: string;
    readonly resultNewParties: string;
    readonly errorTitle: string;
    readonly errorLaunch: string;
    readonly bannerLive: string;
    readonly bannerDemo: string;
    readonly ctaContinue: string;
    readonly ctaStart: string;
    readonly ctaCancel: string;
    readonly ctaClose: string;
    readonly ctaRetry: string;
  };
  readonly backup: {
    readonly exportTitle: string;
    readonly exportIntro: string;
    readonly importTitle: string;
    readonly importIntro: string;
    readonly stepSelect: string;
    readonly stepSelectSub: string;
    readonly stepPassphrase: string;
    readonly stepPassphraseSub: string;
    readonly stepImportPassphraseSub: string;
    readonly stepConfirm: string;
    readonly stepConfirmSub: string;
    readonly stepProgress: string;
    readonly stepProgressSub: string;
    readonly stepDone: string;
    readonly stepDoneSub: string;
    readonly stepError: string;
    readonly stepPaste: string;
    readonly stepPasteSub: string;
    readonly stepPreview: string;
    readonly stepPreviewSub: string;
    readonly walletLabel: string;
    readonly walletMembers: string;
    readonly passphraseLabel: string;
    readonly passphrasePlaceholder: string;
    readonly passphraseRepeat: string;
    readonly passphraseMismatch: string;
    readonly passphraseTooShort: string;
    readonly factsHeading: string;
    readonly factKek: string;
    readonly factKekSub: string;
    readonly factNoPlaintext: string;
    readonly factNoPlaintextSub: string;
    readonly factDevice: string;
    readonly factDeviceSub: string;
    readonly factGroup: string;
    readonly factGroupSub: string;
    readonly progressDeriveKek: string;
    readonly progressDeriveKekSub: string;
    readonly progressWrap: string;
    readonly progressWrapSub: string;
    readonly progressEmit: string;
    readonly progressEmitSub: string;
    readonly progressUnwrap: string;
    readonly progressUnwrapSub: string;
    readonly progressInstall: string;
    readonly progressInstallSub: string;
    readonly progressActive: string;
    readonly progressDone: string;
    readonly progressWait: string;
    readonly resultExportTitle: string;
    readonly resultExportSub: string;
    readonly resultImportTitle: string;
    readonly resultImportSub: string;
    readonly artifactHeading: string;
    readonly artifactSub: string;
    readonly artifactCopy: string;
    readonly artifactCopied: string;
    readonly blobInputLabel: string;
    readonly blobInputPlaceholder: string;
    readonly blobFromClipboard: string;
    readonly blobFromFile: string;
    readonly previewMoniker: string;
    readonly previewSize: string;
    readonly previewCreated: string;
    readonly previewWrap: string;
    readonly previewWrapValue: string;
    readonly errorTitle: string;
    readonly errorPassphrase: string;
    readonly errorBlob: string;
    readonly errorGeneric: string;
    readonly demoBanner: string;
    readonly liveBanner: string;
    readonly ctaContinue: string;
    readonly ctaExport: string;
    readonly ctaImport: string;
    readonly ctaCloseExport: string;
    readonly ctaCloseImport: string;
    readonly ctaRetry: string;
    readonly callShape: string;
    readonly callShapeExport: string;
    readonly callShapeImport: string;
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
  walletDetail: {
    title: 'Wallet detail',
    defaultBadge: 'default',
    xpubReady: 'xpub ready',
    xpubPending: 'xpub pending',
    thresholdHeading: 'Quorum',
    thresholdSub: '{threshold}-of-{parties} signers required',
    membersHeading: 'Members',
    membersSub: '{count} parties · {online} online',
    addressesHeading: 'Addresses',
    addressesSub: '{count} addresses across {chains} chains',
    keyMaterialHeading: 'Key material',
    keyMaterialSub: 'Group public key and chaincode are public; the secret shares stay on each device',
    groupIdLabel: 'groupId',
    ecdsaPubkeyLabel: 'ecdsa pubkey',
    chaincodeLabel: 'chaincode',
    pathLabel: 'path',
    deriveAction: 'Derive new address',
    deriveActionSub: 'Pick a chain and derivation path',
    reshareAction: 'Reshare',
    reshareActionSub: 'Rotate shares while keeping the group public key',
    addressCount: '{count} addresses',
    memberSelfBadge: 'this device',
    memberLastSeen: 'last seen {when}',
  },
  derive: {
    title: 'Derive address',
    subtitle: 'Choose a chain and a path. The address is derived from the existing group public key — no new keygen.',
    chainSection: 'Chain',
    pathSection: 'Derivation path',
    pathSub: 'BIP32-style path. Leave blank to use m/0.',
    previewSection: 'Preview',
    previewSub: 'Re-derived locally from chaincode + group pubkey',
    previewPlaceholder: 'Pick a chain to preview',
    addressLabel: 'address',
    addAction: 'Add to wallet',
    addedTitle: 'Address added',
    addedSub: 'It is now reachable from this wallet for signing requests.',
    closeAction: 'Done',
    demoNote: 'Demo derivation — wired to bridge once exposed',
    duplicateNote: 'This chain + path is already in the wallet',
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
  keygen: {
    title: 'New wallet · keygen',
    intro:
      'Run a t-of-n ECDSA distributed key generation across the committee. The bridge call shape and callback contract from docs/design/mcp/sdk.md §2 are preserved.',
    stepConfigure: 'Configure committee',
    stepConfigureSub: 'Pick the threshold, total parties, and the keystore wrap passphrase for this device.',
    stepConfirm: 'Confirm and start',
    stepConfirmSub: 'Re-read the security facts before launching the DKG run.',
    stepProgress: 'Running keygen',
    stepProgressSub: 'Distributed key generation in progress across the committee.',
    stepDone: 'Committee ready',
    stepError: 'Keygen failed',
    thresholdLabel: 'Threshold (t)',
    thresholdSub: 'Minimum signers required to authorize a transaction.',
    partiesLabel: 'Parties (n)',
    partiesSub: 'Total participating devices in the committee.',
    passphraseLabel: 'Keystore passphrase',
    passphrasePlaceholder: 'At least 4 characters · demo defaults to "demo"',
    passphraseSub: 'Wraps this device’s newly generated share. Never leaves the device.',
    quorumPreview: '{threshold}-of-{parties} signers required',
    factCallback: 'Callback contract',
    factCallbackSub: 'onProgress* fires zero or more times, then exactly one of onResult or onError terminates the run.',
    factGroupPub: 'Group public key',
    factGroupPubSub: 'Surfaced via onResult as a GroupSummary — public, identifies the new committee.',
    factShareLocal: 'Share stays local',
    factShareLocalSub: 'Each device emits its share material; nothing crosses the JS bridge in plaintext.',
    factCommittee: 'Committee identity',
    factCommitteeSub: 'monikers and groupPubKey arrive together when onResult fires.',
    callShape: 'Bridge call shape',
    callShapeValue: 'keyGen({threshold, parties, passphrase}, callbacks) → Promise<void>',
    stageHandshake: 'Handshake',
    stageHandshakeSub: 'Discover committee members, agree on session parameters.',
    stageCommit: 'Round 1 · commitments',
    stageCommitSub: 'Exchange feldman/paillier commitments across signers.',
    stageShare: 'Round 2 · shares',
    stageShareSub: 'Distribute and verify share contributions.',
    stageFinalize: 'Finalize · group pubkey',
    stageFinalizeSub: 'Combine commitments into the invariant group public key.',
    stagePublish: 'Publish · GroupSummary',
    stagePublishSub: 'Emit onResult with threshold / parties / monikers / groupPubKey.',
    stageActive: 'running',
    stageDone: 'done',
    stageWait: 'queued',
    progressEventLabel: 'onProgress',
    resultTitle: 'Committee ready',
    resultSub: 'GroupSummary received via onResult. The committee can now accept signing requests.',
    resultGroupPubKey: 'groupPubKey',
    resultThreshold: 'threshold',
    resultParties: 'parties',
    resultMonikers: 'monikers',
    resultMonikersFallback: '{parties} parties',
    errorTitle: 'Keygen failed',
    errorLaunch: 'launch failed: {detail}',
    bannerLive: 'Live bridge call · keyGen',
    bannerDemo: 'Demo session · bridge stub returned, callbacks not exercised',
    ctaContinue: 'Continue',
    ctaStart: 'Start keygen',
    ctaCancel: 'Cancel',
    ctaClose: 'Done',
    ctaRetry: 'Try again',
  },
  reshare: {
    title: 'Reshare committee',
    intro:
      'Rotate the share material onto a new committee while keeping the group public key — and every derived address — unchanged.',
    invariantTitle: 'Invariant: master pubkey and addresses',
    invariantBody:
      'Reshare rebuilds the secret shares on a new committee but keeps groupPubKey and chaincode identical. Every address derived from the wallet stays valid.',
    stepIntro: 'Read the invariant',
    stepIntroSub: 'Reshare is the safe way to rotate shares — addresses and the group public key never change.',
    stepConfigure: 'Configure new committee',
    stepConfigureSub: 'Old threshold, new threshold, new total parties, and the keystore wrap passphrase.',
    stepConfirm: 'Confirm and start',
    stepConfirmSub: 'Re-read the security facts before launching the reshare run.',
    stepProgress: 'Resharing',
    stepProgressSub: 'New shares are being recombined and installed across the committee.',
    stepDone: 'Shares rotated',
    stepError: 'Reshare failed',
    oldThresholdLabel: 'Old threshold',
    oldThresholdSub: 'The threshold of the existing committee.',
    newThresholdLabel: 'New threshold (t′)',
    newThresholdSub: 'Threshold for the rotated committee.',
    newPartiesLabel: 'New parties (n′)',
    newPartiesSub: 'Total devices in the rotated committee.',
    passphraseLabel: 'Keystore passphrase',
    passphrasePlaceholder: 'At least 4 characters · demo defaults to "demo"',
    passphraseSub: 'Wraps the new share for this device. Never leaves the device.',
    summaryHeading: 'Committee diff',
    oldQuorum: 'old · {threshold}-of-{parties}',
    newQuorum: 'new · {threshold}-of-{parties}',
    factInvariantPub: 'groupPubKey unchanged',
    factInvariantPubSub: 'onResult returns the same groupPubKey as the existing committee.',
    factInvariantAddr: 'Addresses preserved',
    factInvariantAddrSub: 'Every derived chain address remains valid — no on-chain migration needed.',
    factRotate: 'Shares rotated',
    factRotateSub: 'Every device emits new share material; old shares are superseded.',
    factOldWipe: 'Old shares retired',
    factOldWipeSub: 'After install completes, the previous committee can no longer assemble a signature.',
    callShape: 'Bridge call shape',
    callShapeValue: 'reshare({oldT, newT, newN, passphrase}, callbacks) → Promise<void>',
    stagePrepare: 'Prepare',
    stagePrepareSub: 'Lock the existing groupPubKey · open libp2p streams to new committee.',
    stageDistribute: 'Round 1 · distribute',
    stageDistributeSub: 'Old committee splits shares for the new committee.',
    stageRecombine: 'Round 2 · recombine',
    stageRecombineSub: 'New committee reconstructs share contributions privately.',
    stageInstall: 'Install · new shares',
    stageInstallSub: 'Each new device wraps and stores its share under the passphrase KEK.',
    stageVerify: 'Verify · invariant pubkey',
    stageVerifySub: 'Confirm onResult.groupPubKey matches the existing committee.',
    stageActive: 'running',
    stageDone: 'done',
    stageWait: 'queued',
    progressEventLabel: 'onProgress',
    resultTitle: 'Shares rotated',
    resultSub: 'Reshare completed. groupPubKey and every derived address are unchanged.',
    resultGroupPubKey: 'groupPubKey',
    resultOldThreshold: 'old threshold',
    resultNewThreshold: 'new threshold',
    resultNewParties: 'new parties',
    errorTitle: 'Reshare failed',
    errorLaunch: 'launch failed: {detail}',
    bannerLive: 'Live bridge call · reshare',
    bannerDemo: 'Demo session · bridge stub returned, callbacks not exercised',
    ctaContinue: 'Continue',
    ctaStart: 'Start reshare',
    ctaCancel: 'Cancel',
    ctaClose: 'Done',
    ctaRetry: 'Try again',
  },
  backup: {
    exportTitle: 'Export share backup',
    exportIntro:
      'Wrap one of this device’s shares with a passphrase-derived key (Argon2id). The output is an opaque blob — store or share it through a trusted channel.',
    importTitle: 'Import share backup',
    importIntro:
      'Restore a share from a passphrase-wrapped backup blob. The blob is unwrapped on this device and the unwrapped share never leaves the keystore.',
    stepSelect: 'Pick share',
    stepSelectSub: 'Choose which group share to back up',
    stepPassphrase: 'Set wrap passphrase',
    stepPassphraseSub: 'Used only to derive the wrap KEK (Argon2id). It is never sent anywhere.',
    stepImportPassphraseSub: 'Enter the same passphrase used at export time.',
    stepConfirm: 'Confirm',
    stepConfirmSub: 'Re-read the security facts before wrapping the share.',
    stepProgress: 'Working',
    stepProgressSub: 'Driving the bridge call.',
    stepDone: 'Done',
    stepDoneSub: 'Backup ready.',
    stepError: 'Failed',
    stepPaste: 'Paste blob',
    stepPasteSub: 'Paste the base64 backup blob produced by exportShare.',
    stepPreview: 'Preview metadata',
    stepPreviewSub: 'Inspect what the blob will install before confirming.',
    walletLabel: 'Wallet',
    walletMembers: '{threshold}-of-{parties} · {members} members',
    passphraseLabel: 'Wrap passphrase',
    passphrasePlaceholder: 'At least 8 characters · 12+ recommended',
    passphraseRepeat: 'Repeat passphrase',
    passphraseMismatch: 'Passphrases do not match',
    passphraseTooShort: 'Use at least 8 characters',
    factsHeading: 'Security facts',
    factKek: 'Argon2id-wrapped',
    factKekSub: 'Wrap key derived locally from your passphrase. Argon2id default cost.',
    factNoPlaintext: 'No plaintext share',
    factNoPlaintextSub: 'The raw share never leaves the keystore — only the wrapped blob is emitted.',
    factDevice: 'Device identity',
    factDeviceSub: 'Bound to this device · {memberId} on {device}',
    factGroup: 'Group',
    factGroupSub: '{moniker} · {groupId}',
    progressDeriveKek: 'Derive KEK',
    progressDeriveKekSub: 'Argon2id over passphrase + per-share salt',
    progressWrap: 'Wrap share',
    progressWrapSub: 'AEAD seal share material with the derived KEK',
    progressEmit: 'Emit blob',
    progressEmitSub: 'Base64 envelope · safe to copy / store',
    progressUnwrap: 'Unwrap blob',
    progressUnwrapSub: 'AEAD open with derived KEK',
    progressInstall: 'Install share',
    progressInstallSub: 'Store share in keystore under its moniker',
    progressActive: 'running',
    progressDone: 'done',
    progressWait: 'queued',
    resultExportTitle: 'Backup ready',
    resultExportSub: 'The wrapped blob is below. Store it somewhere only you control.',
    resultImportTitle: 'Share installed',
    resultImportSub: 'The share is back in the keystore. The device can re-join its group.',
    artifactHeading: 'Backup blob',
    artifactSub: 'Base64 — Argon2id-wrapped',
    artifactCopy: 'Copy to clipboard',
    artifactCopied: 'Copied · paste into your vault',
    blobInputLabel: 'Backup blob (base64)',
    blobInputPlaceholder: 'Paste the base64 blob here…',
    blobFromClipboard: 'Paste from clipboard',
    blobFromFile: 'Read from file',
    previewMoniker: 'Share moniker',
    previewSize: 'Blob size',
    previewCreated: 'Created',
    previewWrap: 'Wrap',
    previewWrapValue: 'Argon2id · default cost',
    errorTitle: 'Bridge call rejected',
    errorPassphrase: 'Passphrase did not unwrap this blob.',
    errorBlob: 'Blob is empty or malformed.',
    errorGeneric: 'Bridge returned an error.',
    demoBanner: 'Demo session · exportShare/importShare stubs returned a placeholder',
    liveBanner: 'Live bridge call · exportShare/importShare',
    ctaContinue: 'Continue',
    ctaExport: 'Wrap and export',
    ctaImport: 'Unwrap and install',
    ctaCloseExport: 'Back to settings',
    ctaCloseImport: 'Back to settings',
    ctaRetry: 'Try again',
    callShape: 'Bridge call shape',
    callShapeExport: 'exportShare(moniker, passphrase) → blob',
    callShapeImport: 'importShare(blobBase64, passphrase) → moniker',
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
  walletDetail: {
    title: '钱包详情',
    defaultBadge: '默认',
    xpubReady: 'xpub 已就绪',
    xpubPending: 'xpub 未就绪',
    thresholdHeading: '法定人数',
    thresholdSub: '需要 {threshold} / {parties} 方签名',
    membersHeading: '成员',
    membersSub: '共 {count} 方 · 在线 {online}',
    addressesHeading: '地址',
    addressesSub: '{chains} 条链 · 共 {count} 个地址',
    keyMaterialHeading: '密钥材料',
    keyMaterialSub: '组公钥与 chaincode 公开;各方分片永不离开各自设备',
    groupIdLabel: 'groupId',
    ecdsaPubkeyLabel: 'ecdsa 公钥',
    chaincodeLabel: 'chaincode',
    pathLabel: '派生路径',
    deriveAction: '派生新地址',
    deriveActionSub: '选择链与派生路径',
    reshareAction: '重分片 (reshare)',
    reshareActionSub: '轮换分片,保持组公钥不变',
    addressCount: '共 {count} 个地址',
    memberSelfBadge: '本机',
    memberLastSeen: '上次活跃 {when}',
  },
  derive: {
    title: '派生地址',
    subtitle: '选择目标链与派生路径,从现有组公钥重新派生地址 — 无需重新 keygen。',
    chainSection: '链',
    pathSection: '派生路径',
    pathSub: 'BIP32 风格路径,留空时使用 m/0。',
    previewSection: '预览',
    previewSub: '由 chaincode + 组公钥在本机重算',
    previewPlaceholder: '选择一条链查看预览',
    addressLabel: '地址',
    addAction: '加入钱包',
    addedTitle: '已加入',
    addedSub: '该地址现可在本钱包接收签名请求。',
    closeAction: '完成',
    demoNote: '演示派生 — 待 bridge 暴露后接入真实算法',
    duplicateNote: '该链 + 路径已存在于本钱包',
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
  keygen: {
    title: '新建钱包 · keygen',
    intro:
      '在委员会成员之间运行 t-of-n ECDSA 分布式密钥生成。完整保留 docs/design/mcp/sdk.md §2 中的 bridge 调用形态与回调契约。',
    stepConfigure: '配置委员会',
    stepConfigureSub: '选择门限、总参与方数,以及本机 keystore 的封装口令。',
    stepConfirm: '确认并启动',
    stepConfirmSub: '在启动 DKG 之前,再次核对安全事项。',
    stepProgress: '正在 keygen',
    stepProgressSub: '委员会成员协同进行分布式密钥生成。',
    stepDone: '委员会已就绪',
    stepError: 'keygen 失败',
    thresholdLabel: '门限 (t)',
    thresholdSub: '授权一次签名所需的最少签名人数。',
    partiesLabel: '参与方 (n)',
    partiesSub: '本次委员会的总设备数量。',
    passphraseLabel: 'keystore 口令',
    passphrasePlaceholder: '至少 4 位 · 演示默认 "demo"',
    passphraseSub: '用于在本机封装新生成的分片,永不离开设备。',
    quorumPreview: '需要 {threshold} / {parties} 方签名',
    factCallback: '回调契约',
    factCallbackSub: 'onProgress 可触发零次或多次,随后 onResult / onError 之一恰好触发一次以终止本次运行。',
    factGroupPub: '组公钥',
    factGroupPubSub: '在 onResult 中以 GroupSummary 返回 — 公开信息,标识新的委员会。',
    factShareLocal: '分片留在本机',
    factShareLocalSub: '每台设备各自产出自己的分片材料,任何明文分片都不会跨越 JS bridge。',
    factCommittee: '委员会身份',
    factCommitteeSub: 'monikers 与 groupPubKey 会随 onResult 一起返回。',
    callShape: 'Bridge 调用',
    callShapeValue: 'keyGen({threshold, parties, passphrase}, callbacks) → Promise<void>',
    stageHandshake: '握手',
    stageHandshakeSub: '发现委员会成员,协商会话参数。',
    stageCommit: '第 1 轮 · 承诺',
    stageCommitSub: '在签名方之间交换 feldman / paillier 承诺。',
    stageShare: '第 2 轮 · 分片',
    stageShareSub: '分发并校验分片贡献。',
    stageFinalize: '收尾 · 组公钥',
    stageFinalizeSub: '聚合承诺得到不变的组公钥。',
    stagePublish: '发布 · GroupSummary',
    stagePublishSub: '通过 onResult 输出 threshold / parties / monikers / groupPubKey。',
    stageActive: '进行中',
    stageDone: '完成',
    stageWait: '等待',
    progressEventLabel: 'onProgress',
    resultTitle: '委员会已就绪',
    resultSub: 'onResult 返回了 GroupSummary,委员会现在可以接受签名请求。',
    resultGroupPubKey: 'groupPubKey',
    resultThreshold: '门限',
    resultParties: '参与方',
    resultMonikers: 'monikers',
    resultMonikersFallback: '{parties} 方',
    errorTitle: 'keygen 失败',
    errorLaunch: '启动失败: {detail}',
    bannerLive: '已绑定 bridge · keyGen',
    bannerDemo: '演示会话 · bridge 桩返回,未触发回调',
    ctaContinue: '继续',
    ctaStart: '开始 keygen',
    ctaCancel: '取消',
    ctaClose: '完成',
    ctaRetry: '重试',
  },
  reshare: {
    title: '重分片委员会',
    intro:
      '将分片材料轮换到新的委员会上,保持组公钥以及所有派生地址不变。',
    invariantTitle: '不变量:主公钥与地址',
    invariantBody:
      '重分片在新委员会上重建私钥分片,但 groupPubKey 与 chaincode 保持不变。本钱包派生出的每个地址都会继续生效。',
    stepIntro: '阅读不变量',
    stepIntroSub: '重分片是安全的分片轮换方式 — 地址与组公钥永远不变。',
    stepConfigure: '配置新委员会',
    stepConfigureSub: '设置旧门限、新门限、新总参与方数以及 keystore 封装口令。',
    stepConfirm: '确认并启动',
    stepConfirmSub: '在启动重分片之前,再次核对安全事项。',
    stepProgress: '重分片中',
    stepProgressSub: '新分片正在委员会之间重组并安装。',
    stepDone: '分片已轮换',
    stepError: '重分片失败',
    oldThresholdLabel: '旧门限',
    oldThresholdSub: '现有委员会的门限。',
    newThresholdLabel: '新门限 (t′)',
    newThresholdSub: '轮换后委员会的门限。',
    newPartiesLabel: '新参与方 (n′)',
    newPartiesSub: '轮换后委员会的总设备数。',
    passphraseLabel: 'keystore 口令',
    passphrasePlaceholder: '至少 4 位 · 演示默认 "demo"',
    passphraseSub: '用于在本机封装新分片,永不离开设备。',
    summaryHeading: '委员会对比',
    oldQuorum: '旧 · {threshold} / {parties}',
    newQuorum: '新 · {threshold} / {parties}',
    factInvariantPub: 'groupPubKey 不变',
    factInvariantPubSub: 'onResult 返回的 groupPubKey 与现有委员会保持一致。',
    factInvariantAddr: '地址保持不变',
    factInvariantAddrSub: '所有派生的链上地址依然生效,无需任何链上迁移。',
    factRotate: '分片已轮换',
    factRotateSub: '每台设备产出新的分片材料,旧分片被取代。',
    factOldWipe: '旧分片作废',
    factOldWipeSub: '安装完成后,旧委员会再也无法凑齐一次签名。',
    callShape: 'Bridge 调用',
    callShapeValue: 'reshare({oldT, newT, newN, passphrase}, callbacks) → Promise<void>',
    stagePrepare: '准备',
    stagePrepareSub: '锁定现有 groupPubKey · 建立到新委员会的 libp2p 流。',
    stageDistribute: '第 1 轮 · 分发',
    stageDistributeSub: '旧委员会为新委员会切分分片。',
    stageRecombine: '第 2 轮 · 重组',
    stageRecombineSub: '新委员会私下重建分片贡献。',
    stageInstall: '安装 · 新分片',
    stageInstallSub: '每台新设备用口令 KEK 封装并存储新分片。',
    stageVerify: '校验 · 不变的公钥',
    stageVerifySub: '确认 onResult.groupPubKey 与现有委员会一致。',
    stageActive: '进行中',
    stageDone: '完成',
    stageWait: '等待',
    progressEventLabel: 'onProgress',
    resultTitle: '分片已轮换',
    resultSub: '重分片完成。groupPubKey 与所有派生地址均保持不变。',
    resultGroupPubKey: 'groupPubKey',
    resultOldThreshold: '旧门限',
    resultNewThreshold: '新门限',
    resultNewParties: '新参与方',
    errorTitle: '重分片失败',
    errorLaunch: '启动失败: {detail}',
    bannerLive: '已绑定 bridge · reshare',
    bannerDemo: '演示会话 · bridge 桩返回,未触发回调',
    ctaContinue: '继续',
    ctaStart: '开始重分片',
    ctaCancel: '取消',
    ctaClose: '完成',
    ctaRetry: '重试',
  },
  backup: {
    exportTitle: '导出分片备份',
    exportIntro:
      '用口令派生的密钥 (Argon2id) 封装本机持有的某个分片。输出是一个不透明的密文 blob,请通过可信渠道保存或传输。',
    importTitle: '导入分片备份',
    importIntro:
      '从口令封装的备份 blob 中恢复一个分片。本机解封,解封后的明文分片永不离开 keystore。',
    stepSelect: '选择分片',
    stepSelectSub: '选择要备份的分组分片',
    stepPassphrase: '设置封装口令',
    stepPassphraseSub: '仅用于在本机派生封装 KEK (Argon2id),口令本身不会上传。',
    stepImportPassphraseSub: '请输入导出时使用的同一口令。',
    stepConfirm: '确认',
    stepConfirmSub: '在封装分片之前,请再次核对安全事项。',
    stepProgress: '处理中',
    stepProgressSub: '正在调用 bridge 接口。',
    stepDone: '完成',
    stepDoneSub: '备份已就绪。',
    stepError: '失败',
    stepPaste: '粘贴 blob',
    stepPasteSub: '粘贴由 exportShare 生成的 base64 备份 blob。',
    stepPreview: '预览元信息',
    stepPreviewSub: '确认前查看 blob 将要安装的内容。',
    walletLabel: '钱包',
    walletMembers: '{threshold} / {parties} · 共 {members} 方',
    passphraseLabel: '封装口令',
    passphrasePlaceholder: '至少 8 位 · 推荐 12+',
    passphraseRepeat: '再次输入口令',
    passphraseMismatch: '两次输入不一致',
    passphraseTooShort: '请使用至少 8 位口令',
    factsHeading: '安全事项',
    factKek: 'Argon2id 封装',
    factKekSub: '本机用口令 + 每分片盐值派生封装密钥,Argon2id 默认参数。',
    factNoPlaintext: '从不明文',
    factNoPlaintextSub: '明文分片永不离开 keystore,只发出封装后的 blob。',
    factDevice: '设备身份',
    factDeviceSub: '绑定本机 · {memberId} @ {device}',
    factGroup: '分组',
    factGroupSub: '{moniker} · {groupId}',
    progressDeriveKek: '派生 KEK',
    progressDeriveKekSub: '基于口令 + 每分片盐的 Argon2id 派生',
    progressWrap: '封装分片',
    progressWrapSub: '用派生 KEK 进行 AEAD 封装',
    progressEmit: '输出 blob',
    progressEmitSub: 'Base64 信封 · 可安全复制 / 存储',
    progressUnwrap: '解封 blob',
    progressUnwrapSub: '用派生 KEK 进行 AEAD 解封',
    progressInstall: '安装分片',
    progressInstallSub: '按 moniker 存入 keystore',
    progressActive: '进行中',
    progressDone: '完成',
    progressWait: '等待',
    resultExportTitle: '备份已生成',
    resultExportSub: '下方即封装后的 blob。请存放在仅自己可控的位置。',
    resultImportTitle: '分片已安装',
    resultImportSub: '分片已写回 keystore,该设备可重新参与分组签名。',
    artifactHeading: '备份 blob',
    artifactSub: 'Base64 · Argon2id 封装',
    artifactCopy: '复制到剪贴板',
    artifactCopied: '已复制 · 请粘贴到保险位置',
    blobInputLabel: '备份 blob (base64)',
    blobInputPlaceholder: '将 base64 blob 粘贴到此处…',
    blobFromClipboard: '从剪贴板粘贴',
    blobFromFile: '从文件读取',
    previewMoniker: '分片 moniker',
    previewSize: 'Blob 大小',
    previewCreated: '生成时间',
    previewWrap: '封装方式',
    previewWrapValue: 'Argon2id · 默认参数',
    errorTitle: 'Bridge 调用失败',
    errorPassphrase: '口令无法解封此 blob。',
    errorBlob: 'Blob 为空或格式错误。',
    errorGeneric: 'Bridge 返回错误。',
    demoBanner: '演示会话 · exportShare/importShare 桩返回占位结果',
    liveBanner: '已绑定 bridge · exportShare/importShare',
    ctaContinue: '继续',
    ctaExport: '封装并导出',
    ctaImport: '解封并安装',
    ctaCloseExport: '返回设置',
    ctaCloseImport: '返回设置',
    ctaRetry: '重试',
    callShape: 'Bridge 调用',
    callShapeExport: 'exportShare(moniker, passphrase) → blob',
    callShapeImport: 'importShare(blobBase64, passphrase) → moniker',
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
