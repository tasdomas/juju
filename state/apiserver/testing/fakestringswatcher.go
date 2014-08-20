package testing

type FakeStringsWatcher struct {
	ChangeStrings chan []string
}

func (*FakeStringsWatcher) Stop() error {
	return nil
}

func (*FakeStringsWatcher) Kill() {}

func (*FakeStringsWatcher) Wait() error {
	return nil
}

func (*FakeStringsWatcher) Err() error {
	return nil
}

func (w *FakeStringsWatcher) Changes() <-chan []string {
	return w.ChangeStrings
}
