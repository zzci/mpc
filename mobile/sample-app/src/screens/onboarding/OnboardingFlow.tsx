import React, { useState } from 'react';
import { InitIdentityScreen } from './InitIdentityScreen';
import { InitCoordScreen } from './InitCoordScreen';
import { InitConfirmScreen } from './InitConfirmScreen';
import { InitRegisterScreen } from './InitRegisterScreen';
import { InitDoneScreen } from './InitDoneScreen';

export type OnboardingStep = 'identity' | 'coord' | 'confirm' | 'register' | 'done';

export interface OnboardingFlowProps {
  readonly initialStep?: OnboardingStep;
  readonly onExit: () => void;
}

const ORDER: ReadonlyArray<OnboardingStep> = ['identity', 'coord', 'confirm', 'register', 'done'];

export function OnboardingFlow({ initialStep = 'identity', onExit }: OnboardingFlowProps): React.JSX.Element {
  const [step, setStep] = useState<OnboardingStep>(initialStep);

  const goTo = (next: OnboardingStep): void => setStep(next);
  const goBack = (current: OnboardingStep): (() => void) => () => {
    const idx = ORDER.indexOf(current);
    if (idx <= 0) {
      onExit();
      return;
    }
    setStep(ORDER[idx - 1]!);
  };

  if (step === 'identity') {
    return <InitIdentityScreen onBack={onExit} onNext={() => goTo('coord')} />;
  }
  if (step === 'coord') {
    return <InitCoordScreen onBack={goBack('coord')} onNext={() => goTo('confirm')} />;
  }
  if (step === 'confirm') {
    return <InitConfirmScreen onBack={goBack('confirm')} onNext={() => goTo('register')} />;
  }
  if (step === 'register') {
    return <InitRegisterScreen onNext={() => goTo('done')} />;
  }
  return <InitDoneScreen onContinue={onExit} />;
}
