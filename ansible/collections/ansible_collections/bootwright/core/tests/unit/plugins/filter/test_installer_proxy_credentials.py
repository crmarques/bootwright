"""Authentication reaches only the private installer input, encoded literally."""
from ansible_collections.bootwright.core.plugins.filter.installer_proxy_credentials import installer_proxy_credentials


def test_proxy_credentials_are_encoded_and_input_is_unchanged():
    config = {"proxy": {"httpProxy": "http://[2001:db8::1]:3128", "httpsProxy": "https://proxy.example.test", "noProxy": "*"}}
    result = installer_proxy_credentials(config, {"credentialsRef": "auth"}, "a@b", "p:/ #")
    assert result["proxy"]["httpProxy"] == "http://a%40b:p%3A%2F%20%23@[2001:db8::1]:3128"
    assert result["proxy"]["httpsProxy"] == "https://a%40b:p%3A%2F%20%23@proxy.example.test"
    assert result["proxy"]["noProxy"] == "*"
    assert config["proxy"]["httpProxy"] == "http://[2001:db8::1]:3128"
