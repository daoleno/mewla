import { effect, Scroll, signal, Text, untracked, View } from "nsh";
import { BusyPlace } from "../kit/wait";
import { Button, Input } from "nsh/base";
import { AppSheet } from "../shell/AppSheet";
import type { Daemon } from "../../core/more/daemon";
import { codexSkills, filterCodexSkills, type CodexSkill } from "../../core/session/sessionRequests";
import { Icon } from "../icons/Icon";

/**
 * Codex's skills for the Session's folder (`/skills`): a search over name,
 * description, scope and path, and the list. A tap picks one: `onPick(name)`
 * (the composer inserts `insertSkillMention(draft, name)`), then the sheet
 * closes. A disabled skill is dimmed but can still be picked. Each opening
 * starts with an empty search and reads the list again.
 */
export function CodexSkillsSheet(props: {
  open: () => boolean;
  onClose: () => void;
  transport: Daemon;
  /** The Session's folder; the computer's default without it. */
  cwd?: string;
  onPick: (name: string) => void;
}) {
  const query = signal("");
  const skills = signal<CodexSkill[]>([]);
  const loading = signal(false);
  const problem = signal("");
  let generation = 0;

  const load = async () => {
    const mine = ++generation;
    query.set("");
    loading.set(true);
    problem.set("");
    try {
      const snapshot = await codexSkills(props.transport, props.cwd);
      if (mine === generation) skills.set(snapshot.skills);
    } catch (error) {
      if (mine !== generation) return;
      skills.set([]);
      problem.set(error instanceof Error && error.message ? error.message : "Could not load skills.");
    } finally {
      if (mine === generation) loading.set(false);
    }
  };

  effect(() => {
    if (props.open()) untracked(() => void load());
    else generation++;
  });

  const visible = () => filterCodexSkills(skills(), query());
  const pick = (skill: CodexSkill) => {
    props.onPick(skill.name);
    props.onClose();
  };

  return (
    <AppSheet open={props.open()} onOpenChange={({ open }) => !open && props.onClose()} closeOnSwipe popupClass="rounded-t-2xl bg-background px-4 pt-4" backdropClass="bg-black/40">
      <View testID="skills-sheet" class="gap-3 pb-8">
        <View class="flex-row items-center gap-2">
          <Text class="flex-1 text-lg font-semibold">Skills</Text>
          <Button testID="skills-close" accessibilityLabel="Close skills" onPress={props.onClose} class="rounded-full p-1.5 active:bg-accent">
            <Icon name="close" size={20} color="foreground" />
          </Button>
        </View>
        <View class="flex-row items-center gap-2 rounded-xl border border-border bg-card px-3">
          <Icon name="search" size={16} />
          <Input testID="skills-search" accessibilityLabel="Search skills" value={query()} placeholder="Search skills" autoFocus autoCapitalize="none" autoCorrect={false}
            returnKeyType="search" onChange={({ value }) => query.set(value)} class="flex-1 py-2 text-base" />
          <If when={query()}><Button testID="skills-search-clear" accessibilityLabel="Clear skill search" onPress={() => query.set("")} class="p-1">
              <Icon name="close-circle-fill" size={16} />
            </Button></If>
        </View>

        <If when={loading()} else={<If when={problem()} else={<If when={visible().length === 0} else={<Scroll keyboardShouldPersistTaps="handled" style={{ maxHeight: 430 }}>
            <View class="overflow-hidden rounded-xl border border-border bg-card">
              {visible().map((skill) => (
                <Button testID={`skill-${skill.name}`} accessibilityLabel={`Use ${skill.name}`} onPress={() => pick(skill)}
                  class="flex-row items-center gap-3 border-b border-border px-3 py-2.5 active:bg-accent">
                  <Icon name="add" size={18} color={skill.enabled ? "primary" : "muted-foreground"} />
                  <View class={skill.enabled ? "flex-1 gap-0.5" : "flex-1 gap-0.5 opacity-60"}>
                    <Text class="text-base line-clamp-1">{skill.name}</Text>
                    <If when={skill.description}>{(description) => <Text class="text-xs text-muted-foreground line-clamp-2">{description()}</Text>}</If>
                  </View>
                  <Text class="text-xs text-muted-foreground">{skill.scope}</Text>
                </Button>
              ))}
            </View>
          </Scroll>}><Text class="py-10 text-center text-sm text-muted-foreground">No matching skills</Text></If>}><View testID="skills-failed" class="items-center gap-1 py-8">
            <Text class="text-base font-semibold text-destructive">Couldn't load skills</Text>
            <Text class="text-center text-sm text-muted-foreground">{problem()}</Text>
          </View></If>}><BusyPlace label="Reading skills" testID="skills-loading" rows={4} /></If>
      </View>
    </AppSheet>
  );
}
