from gateway_client import RunMeta, _try_update_meta


def test_try_update_meta_picks_nested_metadata_fields():
    meta = RunMeta()
    evt = {
        "object": "message.delta",
        "content": {
            "type": "text",
            "text": {"value": "hi"},
            "metadata": {"requestId": "r1", "taskId": "t1", "uniqueCode": "u1"},
        },
    }
    _try_update_meta(meta, evt)
    assert meta.request_id == "r1"
    assert meta.task_id == "t1"
    assert meta.unique_code == "u1"

