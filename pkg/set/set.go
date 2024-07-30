package set

type Set[E comparable] map[E]struct{}

func New[E comparable](vals ...E) Set[E] {
	s := Set[E]{}
	for _, v := range vals {
		s[v] = struct{}{}
	}
	return s
}

func (s Set[E]) Add(vals ...E) {
	for _, v := range vals {
		s[v] = struct{}{}
	}
}

func (s Set[E]) Equals(other Set[E]) bool {
	if len(s) != len(other) {
		return false
	}
	for k := range s {
		if _, has := other[k]; !has {
			return false
		}
	}
	return true
}
