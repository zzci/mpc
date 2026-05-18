// WYSIWYS approval sheet — the human decision surface of the signing flow
// (docs/design/mcp/sdk.md §3). It renders the three onDecoded payloads in
// separate zones and exposes this device's Approve / Reject, which are a
// host→Go reverse call on the SignSession (DREV-001 D4-1), never a callback.
//
// Multi-party demo structure: each committee member reviews and approves on
// their OWN device; coord gates START on a quorum (architecture.md §4 /
// contract/api.md B). This sheet shows the committee roster with this
// device highlighted so the example conveys the t-of-n shape; the other
// parties' decisions are not simulated here (B-005 scope: skeleton only,
// no run, no device).

import React from 'react';
import { Text, View, Button } from 'react-native';

export interface DecodedView {
  /** A-zone: digest-bound, re-derived facts — the sole funds-safety
   *  authority (docs/design/mcp/sdk.md §4). */
  aFacts: unknown;
  /** B-zone: proposer-supplied businessInfo, advisory only. */
  bInfo: unknown;
  /** Declarative A/B mismatches; non-empty ⇒ prominent warning. */
  mismatch: unknown;
}

export interface ApprovalSheetProps {
  decoded: DecodedView;
  committee: string[];
  thisDevice: string;
  onApprove: () => void;
  onReject: () => void;
}

function zone(label: string, payload: unknown): React.JSX.Element {
  return (
    <View>
      <Text>{label}</Text>
      <Text>{JSON.stringify(payload)}</Text>
    </View>
  );
}

export default function ApprovalSheet(
  props: ApprovalSheetProps,
): React.JSX.Element {
  const { decoded, committee, thisDevice, onApprove, onReject } = props;
  const hasMismatch =
    Array.isArray(decoded.mismatch) && decoded.mismatch.length > 0;

  return (
    <View>
      <Text>Review &amp; sign (WYSIWYS)</Text>
      {zone('A — verified facts (authoritative)', decoded.aFacts)}
      {zone('B — business info (advisory)', decoded.bInfo)}
      {hasMismatch && zone('⚠ A/B MISMATCH', decoded.mismatch)}

      <Text>Committee (t-of-n, each approves on its own device):</Text>
      {committee.map((m) => (
        <Text key={m}>
          {m === thisDevice ? '→ ' : '  '}
          {m}
          {m === thisDevice ? ' (this device)' : ''}
        </Text>
      ))}

      <Button title="Approve" onPress={onApprove} />
      <Button title="Reject" onPress={onReject} />
    </View>
  );
}
