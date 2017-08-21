## Welcome!

We're so glad you're thinking about contributing to an 18F open source project! If you're unsure about anything, just ask -- or submit the issue or pull request anyway. The worst that can happen is you'll be politely asked to change something. We love all friendly contributions.

We want to ensure a welcoming environment for all of our projects. Our staff follow the [18F Code of Conduct](https://github.com/18F/code-of-conduct/blob/master/code-of-conduct.md) and all contributors should do the same.

We encourage you to read this project's CONTRIBUTING policy (you are here), its [LICENSE](LICENSE.md), and its [README](README.md).

If you have any questions or want to read more, check out the [18F Open Source Policy GitHub repository](https://github.com/18f/open-source-policy), or just [shoot us an email](mailto:18f@gsa.gov).

## Testing

### Template tests

The template tests are useful to see what a completed template looks like. When developing, you
should add your expected result file in the `testdata` folder. The file should be a `.html` file
so that when a user opens it, it will open in their browser.

In the case, there's a failed template test, the failed rendered result will be in the same folder
as the expected result but with a `.html.returned` file name.

- A test data development common pattern is to create an empty expected `.html`. Upon running the
test, it will render the result with a `.html.returned` file name. You can `mv` the file to the
expected `.html` file to overwrite the existing empty one.

## Public domain

This project is in the public domain within the United States, and
copyright and related rights in the work worldwide are waived through
the [CC0 1.0 Universal public domain dedication](https://creativecommons.org/publicdomain/zero/1.0/).

All contributions to this project will be released under the CC0
dedication. By submitting a pull request, you are agreeing to comply
with this waiver of copyright interest.
