import { PhoneLink } from "./phoneLink";
import { PhoneLinkHost } from "./phoneLinkHost";

/** The process's one phone link (see PhoneLinkHost). */
export const phoneLinkHost = new PhoneLinkHost((options) => new PhoneLink(options));
