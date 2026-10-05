// Shared page behaviour: reveal on scroll, the "needs you" seal stamp.
document.documentElement.classList.add("js");
const io = "IntersectionObserver" in window
  ? new IntersectionObserver((entries) => {
      for (const e of entries) if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); }
    }, { rootMargin: "0px 0px -12% 0px" })
  : null;
for (const el of document.querySelectorAll(".rv, [data-stamp], .brush-rule")) io ? io.observe(el) : el.classList.add("in");
