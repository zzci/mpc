import React, { useMemo } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import {
  Card,
  Hairline,
  Icon,
  Screen,
  TopBar,
  useTheme,
  spacing,
  radius,
  fontFamily,
} from '../../ui';
import type { ThemeTokens } from '../../ui';
import { useI18n } from '../../i18n';
import type { Strings } from '../../i18n';
import { AUDIT } from '../../data';
import type { AuditEvent, AuditOp } from '../../data';

export function AuditScreen(): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const stats = useMemo(() => buildStats(t, T), [t, T]);
  const byDate = useMemo(() => groupByDate(AUDIT), []);

  return (
    <Screen>
      <TopBar
        title={T.audit.title}
        right={
          <View style={s.iconBtn}>
            <Icon name="more" size={16} color={t.text} />
          </View>
        }
      />
      <View style={s.body}>
        <View style={s.statsRow}>
          {stats.map((sp) => (
            <View key={sp.label} style={s.statCard}>
              <Text style={[s.statValue, { color: sp.color }]}>{sp.value}</Text>
              <Text style={s.statLabel}>{sp.label}</Text>
            </View>
          ))}
        </View>

        {byDate.map((bucket) => (
          <View key={bucket.date} style={s.section}>
            <Text style={s.sectionHeading}>{bucket.date}</Text>
            <Card style={s.eventsCard}>
              {bucket.events.map((ev, i) => (
                <View key={ev.requestId}>
                  <EventRow event={ev} />
                  {i < bucket.events.length - 1 ? <Hairline /> : null}
                </View>
              ))}
            </Card>
          </View>
        ))}
      </View>
    </Screen>
  );
}

interface StatSpec {
  readonly label: string;
  readonly value: number;
  readonly color: string;
}

function buildStats(t: ThemeTokens, T: Strings): ReadonlyArray<StatSpec> {
  return [
    { label: T.audit.signed, value: AUDIT.filter((r) => r.op === 'signed').length, color: t.accent },
    { label: T.audit.rejected, value: AUDIT.filter((r) => r.op === 'rejected').length, color: t.danger },
    { label: T.audit.expired, value: AUDIT.filter((r) => r.op === 'expired').length, color: t.warn },
  ];
}

interface AuditBucket {
  readonly date: string;
  readonly events: ReadonlyArray<AuditEvent>;
}

function groupByDate(events: ReadonlyArray<AuditEvent>): ReadonlyArray<AuditBucket> {
  const order: string[] = [];
  const buckets = new Map<string, AuditEvent[]>();
  for (const e of events) {
    if (!buckets.has(e.d)) {
      buckets.set(e.d, []);
      order.push(e.d);
    }
    buckets.get(e.d)!.push(e);
  }
  return order.map((d) => ({ date: d, events: buckets.get(d) ?? [] }));
}

interface EventRowProps {
  readonly event: AuditEvent;
}

function EventRow({ event }: EventRowProps): React.JSX.Element {
  const t = useTheme();
  const { t: T } = useI18n();
  const s = useMemo(() => makeStyles(t), [t]);
  const cfg = opConfig(event.op, t, T);
  return (
    <View style={s.row}>
      <View style={[s.rowIcon, { backgroundColor: `${cfg.color}1a`, borderColor: `${cfg.color}55` }]}>
        <Icon name={cfg.icon} size={14} color={cfg.color} />
      </View>
      <View style={s.rowBody}>
        <View style={s.rowTitleLine}>
          <Text style={s.rowTitle}>{cfg.label}</Text>
          <Text style={s.rowReqId}>{event.requestId}</Text>
        </View>
        <Text style={s.rowAmount} numberOfLines={1}>
          {event.value} → <Text style={s.mono}>{event.to}</Text>
        </Text>
        <Text style={s.rowMeta}>
          {event.t} · {event.group} · {event.chain}
        </Text>
        {event.rsv ? (
          <View style={s.rsvBox}>
            <Icon name="shield" size={11} color={t.accent} />
            <Text style={s.rsvText}>RSV: {event.rsv}</Text>
          </View>
        ) : null}
        {event.reason ? <Text style={[s.reason, { color: cfg.color }]}>{event.reason}</Text> : null}
      </View>
    </View>
  );
}

interface OpConfig {
  readonly label: string;
  readonly icon: 'check' | 'close' | 'clock';
  readonly color: string;
}

function opConfig(op: AuditOp, t: ThemeTokens, T: Strings): OpConfig {
  if (op === 'signed') return { label: T.audit.signed, icon: 'check', color: t.accent };
  if (op === 'rejected') return { label: T.audit.rejected, icon: 'close', color: t.danger };
  return { label: T.audit.expired, icon: 'clock', color: t.warn };
}

function makeStyles(t: ThemeTokens) {
  return StyleSheet.create({
    iconBtn: {
      width: 36,
      height: 36,
      borderRadius: 18,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    body: { paddingHorizontal: spacing.lg, paddingTop: spacing.xs },
    statsRow: { flexDirection: 'row', gap: 8, marginBottom: 18 },
    statCard: {
      flex: 1,
      padding: 11,
      borderRadius: radius.lg,
      backgroundColor: t.surface,
      borderColor: t.hairline,
      borderWidth: StyleSheet.hairlineWidth,
    },
    statValue: { fontSize: 22, fontWeight: '700' },
    statLabel: { color: t.text3, fontSize: 10.5, marginTop: 2 },
    section: { marginBottom: 18 },
    sectionHeading: {
      color: t.text2,
      fontSize: 11.5,
      fontWeight: '700',
      letterSpacing: 0.6,
      textTransform: 'uppercase',
      marginBottom: 8,
      paddingHorizontal: 4,
    },
    eventsCard: { padding: 0 },
    row: { flexDirection: 'row', padding: 12, gap: 11 },
    rowIcon: {
      width: 30,
      height: 30,
      borderRadius: 9,
      borderWidth: StyleSheet.hairlineWidth,
      alignItems: 'center',
      justifyContent: 'center',
    },
    rowBody: { flex: 1, minWidth: 0 },
    rowTitleLine: { flexDirection: 'row', alignItems: 'center', gap: 6, marginBottom: 2 },
    rowTitle: { color: t.text, fontSize: 13, fontWeight: '700' },
    rowReqId: { color: t.text3, fontSize: 10.5, fontFamily: fontFamily.mono },
    rowAmount: { color: t.text2, fontSize: 12, marginBottom: 3 },
    rowMeta: { color: t.text3, fontSize: 10.5 },
    mono: { fontFamily: fontFamily.mono },
    rsvBox: {
      marginTop: 6,
      flexDirection: 'row',
      alignItems: 'center',
      gap: 5,
      paddingHorizontal: 8,
      paddingVertical: 4,
      borderRadius: 6,
      backgroundColor: t.surface2,
      alignSelf: 'flex-start',
    },
    rsvText: { color: t.text2, fontSize: 10.5, fontFamily: fontFamily.mono },
    reason: { fontSize: 10.5, marginTop: 6 },
  });
}
