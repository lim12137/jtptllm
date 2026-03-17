import time

from openai_proxy_server import SessionManager


class DummyClient:
    def __init__(self) -> None:
        self.created = 0
        self.deleted = []

    def create_session(self) -> str:
        self.created += 1
        return f"s{self.created}"

    def delete_session(self, session_id: str) -> bool:
        self.deleted.append(session_id)
        return True


def test_session_manager_reuses_session_within_ttl():
    client = DummyClient()
    mgr = SessionManager(client, ttl_s=600)
    s1 = mgr.get_or_create("k1")
    s2 = mgr.get_or_create("k1")
    assert s1 == s2
    assert client.created == 1


def test_session_manager_expires_and_recreates():
    client = DummyClient()
    mgr = SessionManager(client, ttl_s=1)
    s1 = mgr.get_or_create("k1")
    # force expiry
    mgr._sessions["k1"]["last"] = time.monotonic() - 2
    s2 = mgr.get_or_create("k1")
    assert s1 != s2
    assert client.created == 2
    assert s1 in client.deleted


def test_session_manager_invalidate():
    client = DummyClient()
    mgr = SessionManager(client, ttl_s=600)
    s1 = mgr.get_or_create("k1")
    mgr.invalidate("k1")
    assert s1 in client.deleted
