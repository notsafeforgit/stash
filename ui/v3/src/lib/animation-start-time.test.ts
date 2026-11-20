import { afterEach, beforeEach, expect, it, vi } from "vitest";

function fakeAnimationClass() {
  return class FakeAnimation {
    timelineTime = 500;
    playbackRate = 1;
    start: number | null = null;
    seeks: number[] = [];
    get startTime() {
      return this.start;
    }
    set startTime(value: number | null) {
      this.start = value;
    }
    get currentTime() {
      return this.start === null ? null : this.timelineTime - this.start;
    }
    set currentTime(value: number | null) {
      if (value === null) return;
      this.seeks.push(value);
      this.start = this.timelineTime - value;
    }
  };
}

let FakeAnimation: ReturnType<typeof fakeAnimationClass>;
beforeEach(async () => {
  vi.resetModules();
  FakeAnimation = fakeAnimationClass();
  vi.stubGlobal("Animation", FakeAnimation);
  const { installAnimationStartTimeSync } = await import(
    "./animation-start-time"
  );
  installAnimationStartTimeSync();
  installAnimationStartTimeSync();
});
afterEach(() => vi.unstubAllGlobals());

it("re-seeks once to the same time after each start time change", () => {
  const animation = new FakeAnimation();
  animation.startTime = 1500;
  animation.startTime = 480;
  expect(animation.seeks).toEqual([-1000, 20]);
  expect(animation.startTime).toBe(480);
});

it("leaves cleared and stopped animations alone", () => {
  const animation = new FakeAnimation();
  animation.startTime = null;
  animation.playbackRate = 0;
  animation.startTime = 480;
  expect(animation.seeks).toEqual([]);
  expect(animation.startTime).toBe(480);
});
