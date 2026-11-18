import { signal } from "nsh";
import type { Trigger } from "../../core/triggers/model";
import { TriggersModel } from "../../core/triggers/triggers";
import { onTransport } from "../session";

/** The current computer's Triggers (core/triggers); null without a computer. */
export const triggersModel = signal<TriggersModel | null>(null);
export const triggers = signal<Trigger[]>([]);

onTransport((transport, server) => {
  const model = new TriggersModel(server.id, transport);
  triggersModel.set(model);
  const stop = model.onChange(() => triggers.set([...model.triggers]));
  return () => {
    stop();
    model.close();
    triggersModel.set(null);
    triggers.set([]);
  };
});
