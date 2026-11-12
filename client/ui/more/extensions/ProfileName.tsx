import { signal, untracked, View } from "nsh";
import { suggestedAWSProfile, validAWSProfileName } from "../../../core/extensions/cloudProfilesModel";
import type { PluginAccount } from "../../../core/extensions/connections";
import { Field, Notice } from "../../kit/kit";
import { Action } from "./Action";
import { flowState, plugins } from "./plugins";

/**
 * AWS: the profile's name in the AWS CLI, which the person may change. Right
 * after a sign-in (`suggest`) it offers a readable name from the account's
 * alias and role instead of the CLI's own. Saved once, whatever is tapped.
 */
export function ProfileName(props: { account: PluginAccount; suggest?: boolean; onDone: () => void }) {
  const name = signal(untracked(() => (props.suggest ? suggestedAWSProfile(props.account) : props.account.profile || props.account.name)));
  const error = signal("");
  const saving = signal(false);
  const save = async () => {
    if (saving()) return;
    const value = name().trim();
    if (!validAWSProfileName(value)) return error.set("Use letters, numbers, dots, underscores or hyphens.");
    saving.set(true);
    error.set("");
    try {
      const saved = await plugins()?.renameProfile(props.account, value);
      if (saved) props.onDone();
      else error.set(flowState().error || "Couldn't rename this profile. Check the name and try again.");
    } catch (failure) {
      error.set(failure instanceof Error ? failure.message : "Couldn't rename this profile. Try again.");
    } finally {
      saving.set(false);
    }
  };
  return (
    <View testID="profile-name" class="pb-3">
      <Field label="Profile name" testID="profile-name-field" value={name()} onChange={(value) => { name.set(value.slice(0, 128)); error.set(""); }} onSubmit={() => void save()} />
      <View class="gap-2 px-4">
        {error() ? <Notice live tone="failed" title="Check the name" detail={error()} /> : null}
        <Action testID="profile-name-save" filled={!props.suggest} label="Save name" busy={saving()} onPress={() => void save()} />
      </View>
    </View>
  );
}
