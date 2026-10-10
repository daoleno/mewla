package workerproc

// descendants walks the process tree below root breadth first, visiting each
// pid once.
func descendants(root int, children func(int) []int) []int {
	if root <= 0 {
		return nil
	}
	seen := map[int]bool{root: true}
	queue := []int{root}
	var out []int
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, child := range children(pid) {
			if seen[child] {
				continue
			}
			seen[child] = true
			out = append(out, child)
			queue = append(queue, child)
		}
	}
	return out
}
